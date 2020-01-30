package ws

import (
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

// Wait 20 seconds longer the server ping then fail
var clientPingPeriod = pingPeriod + (20 * time.Second)

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

	go wsConn.clientPingLoop()

	log.WithFields(log.Fields{"clientId": wsConn.ClientId, "remoteIP": wsConn.RemoteIP}).Info("WS client dial success")
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
				go wsConn.clientPingLoop()
				log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Info("WS client redial successful ")
				return
			}
			conn.Close()
		}
		log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Error("WS redial: ", err)
		time.Sleep(time.Second * 30)
	}
}

// Close will terminate the connection with the remote peer. This is the normal scenario.
// This informs the peer of a normal closure.
func (wsConn *WSConn) Close() {

	wsConn.writeMutex.Lock()
	defer wsConn.writeMutex.Unlock()

	if !wsConn.closing {
		log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Info("WS close normal")
		// wsConn.closing = true
		wsConn.c.SetWriteDeadline(time.Now().Add(writeWait))
		wsConn.c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		time.Sleep(closeGracePeriod)

		wsConn.c.Close()
		return
	}

	log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Info("WS close already in progress")
}

// clientClose is invoked by the background reader goroutine when the ws fails or is closed.
func (wsConn *WSConn) clientClose() {

	wsConn.writeMutex.Lock()
	defer wsConn.writeMutex.Unlock()

	if !wsConn.closing {
		log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Info("WS client close")
		wsConn.closing = true
		wsConn.c.Close()
		wsConn.callback.Closed(wsConn)
	}
}

func (wsConn *WSConn) sendAuthentication() (err error) {

	// log.Info("WS client authentication start ", wsConn.ClientId)
	msg, err := Encode(msgAuthentication, nil, &wsConn.ClientId)
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

	if err = msg.Decode(nil, &wsConn.RemoteIP); err != nil {
		log.Error("WS client dial unexpected remote IP", err, msg, seq)
		return err
	}

	log.Debugf("WS client dial authentication successful %v remote IP %v", wsConn.ClientId, wsConn.RemoteIP)
	return nil
}

func (wsConn *WSConn) clientReaderLoop(process func(clientId string, msg WSMsg) (response WSMsg, err error)) {
	defer wsConn.clientClose()
	// defer log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS client goroutine ended")

	for {
		log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS client read")
		msg, err := wsConn.read()
		if err != nil {
			// abnormal closure & not closing
			if err != ErrorClosed && !wsConn.closing {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS client failed to read websocket message", err)
				if AutoRedial {
					// wsConn.c.Close() // close the underlying WS; this will stop ping goroutine.
					wsConn.redialLoop()

					// Run this call back in a goroutine because THIS reader loop must be running to receive responses.
					go wsConn.callback.AfterRedial(wsConn)
					continue
				}
			}
			wsConn.clientClose()
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
			wsConn.c.Close()
			continue
		}

		if response.Type() != msgNoResponse {
			log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS client writing response ", response)
			_, err := wsConn.Write(response)
			if err != nil {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS client error in response")
				continue
			}
		}
	}

}

func (wsConn *WSConn) clientPingLoop() {
	wsConn.lastUpdated = time.Now()

	// Important: Keep a copy of wsConn.c; wsConn.c will change during redial
	conn := wsConn.c

	conn.SetPingHandler(func(msg string) error {
		wsConn.lastUpdated = time.Now()
		log.Info("WS client PING recv")

		// log.WithFields(log.Fields{"clientID": conn.ClientId}).Info("WS server pinging")
		wsConn.writeMutex.Lock()
		err := conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(writeWait))
		wsConn.writeMutex.Unlock()
		if err != nil {
			log.Println("WS client failed to send PONG", err)
		}
		return nil
	})

	for {
		time.Sleep(clientPingPeriod)

		deadline := time.Now().Add(clientPingPeriod * 2 * -1)
		if wsConn.lastUpdated.Before(deadline) {
			if !wsConn.closing {
				log.WithFields(log.Fields{"clientid": wsConn.ClientId}).Error("WS client PING failed")
				conn.Close() // wakeup reader goroutine to handle the error
			}
			return
		}
	}
}
