package ws

import (
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"net/url"
	"spinifex/base"
	"time"
)

// func WebSocketDial(url url.URL, readChannel chan<- []byte) (wsConn *WSConn, err error) {
//
func WebSocketDial(url url.URL, clientId string, handler WSClient) (wsConn *WSConn, err error) {
	log.WithFields(log.Fields{"clientId": clientId, "server": url.String()}).Debug("WS client dial")

	wsConn = &WSConn{url: url, ClientId: clientId, callback: handler}
	wsConn.c, _, err = websocket.DefaultDialer.Dial(url.String(), nil)
	if err != nil {
		log.Error("dial:", err)
		return nil, err
	}

	wsConn.callback = handler
	wsConn.readChannel = make(chan WSMsg, 16)

	if err = wsConn.sendAuthentication(); err != nil {
		log.Error("dial:", err)
		return nil, err
	}

	// Client reader goroutine
	go wsConn.clientReaderLoop(wsConn.callback.Process)

	log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Info("WS client dial success")
	return wsConn, nil
}

func (wsConn *WSConn) redialLoop() {
	for {
		log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Info("WS client redial ")
		conn, _, err := websocket.DefaultDialer.Dial(wsConn.url.String(), nil)
		if err == nil {
			wsConn2 := &WSConn{c: conn, ClientId: wsConn.ClientId}
			if err = wsConn2.sendAuthentication(); err == nil {
				wsConn.c = conn
				log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Info("WS client redial successful ")
				return
			}
			conn.Close()
		}
		log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Error("WS redial: ", err)
		time.Sleep(time.Second * 30)
	}
}

func (wsConn *WSConn) sendAuthentication() (err error) {

	// log.Info("WS client authentication start ", wsConn.ClientId)
	msg, err := Encode(msgAuthentication, &wsConn.ClientId)
	if err != nil {
		log.Error("WS client dial could not encode auth msg", err)
		return err
	}

	// log.Info("WS client authentication write ", msg)
	seq, err := wsConn.Write(msg)
	if err != nil {
		return err
	}

	// log.Info("WS client authentication read response")
	msg, err = wsConn.read()
	if err != nil || msg.Type() != msgAuthentication || msg.Sequence() != seq {
		log.Error("WS client dial did not receive auth response", err, msg, seq)
		return err
	}

	log.Debug("WS client dial authentication successful ", wsConn.ClientId)
	return nil
}

func (wsConn *WSConn) clientClose() {
	log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS clientClose socket")
	wsConn.c.Close()
}

func (wsConn *WSConn) clientReaderLoop(process func(clientId string, msg WSMsg) (response WSMsg, err error)) {
	defer wsConn.clientClose()
	// defer log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS client goroutine ended")

	for {
		log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS client read")
		msg, err := wsConn.read()
		if err != nil {
			if err != base.ErrorClosed {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS client failed to read websocket message", err)
				wsConn.redialLoop()
				continue
			}
			return
		}

		log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS client ", msg)
		// If response, there will be a goroutine waiting
		if msg.IsResponse() {
			if channelIsClosed(wsConn.readChannel) {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS client channel is closed")
				return
			}

			log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS client received response msg ", msg)
			wsConn.readChannel <- msg
			continue
		}

		// log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS client start process msg ", msg)
		response, err := process(wsConn.ClientId, msg)
		if err != nil {
			log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS client process error")
			return
		}
		if response.Type() != msgNoResponse {
			log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS client writing response ", response)
			_, err := wsConn.Write(response)
			if err != nil {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server error in response")
				return
			}
		}
	}

}

/***
func (wsConn *WSConn) dontkeepAlive() {
	pongReceived := false
	wsConn.c.SetPongHandler(func(msg string) error {
		pongReceived = true
		log.Info("PONG")
		return nil
	})

	go func() {
		defer log.WithFields(log.Fields{"clientid": wsConn.ClientId}).Error("PING goroutine terminated")

		for {
			pongReceived = false
			log.WithFields(log.Fields{"clientid": wsConn.ClientId}).Info("PING")
			wsConn.writeMutex.Lock()
			//err := wsConn.c.WriteMessage(websocket.PingMessage, []byte("keepalive"))
			err := wsConn.c.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(writeWait))
			wsConn.writeMutex.Unlock()
			if err != nil {
				log.WithFields(log.Fields{"clientid": wsConn.ClientId}).Error("WS failed to write PING", err)
				wsConn.Close()
				return
			}
			time.Sleep(pingPeriod)
			if !pongReceived {
				log.WithFields(log.Fields{"clientid": wsConn.ClientId}).Error("WS PING timed out - closing websocket")
				wsConn.Close()
				return
			}
		}
	}()
}
***/
