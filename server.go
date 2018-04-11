package ws

import (
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"net"
	"net/http"
	"spinifex/base"
	"sync"
	"time"
)

var (
	wsMutex      sync.Mutex
	webSocketMap map[string]*WSConn = make(map[string]*WSConn, 128)
	// How often do ping
	pingPeriod = 30 * time.Second
)

func init() {
	go serverPingLoop()
}

func (wsConn *WSConn) serverClose() {
	log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Info("WS serverClose socket")

	wsConn.c.Close()

	// Delete entry from online table
	// ONLY if the WS has not restablished for the same device ID name
	wsMutex.Lock()
	ws := webSocketMap[wsConn.ClientId]
	if ws == nil || ws != wsConn {
		log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server deleting websocket map entry")
		wsMutex.Unlock()
		return
	}
	delete(webSocketMap, wsConn.ClientId)
	wsMutex.Unlock()
}

func (wsConn *WSConn) serverReaderLoop(process func(clientId string, msg WSMsg) (response WSMsg, err error)) {
	// defer log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server goroutine ended")
	defer wsConn.serverClose()

	// wsConn.c.SetReadDeadline(time.Now().Add(writeWait))
	// wsConn.c.SetReadDeadline(0)

	for {
		log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS Server read")
		msg, err := wsConn.read()
		if err != nil {
			if err != base.ErrorClosed {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server failed to read websocket message", err)
			}
			return
		}

		// If response, there will be a goroutine waiting
		if msg.IsResponse() {
			if channelIsClosed(wsConn.readChannel) {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server channel is closed")
				return
			}

			log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Infof("WS server got response type %v len %v", msg.Type(), len(msg))
			wsConn.readChannel <- msg
			continue
		}

		switch msg.Type() {
		case msgAuthentication:
			log.Error("websocket Unexpected authentication message")

		default:
			log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Infof("WS server process msg seq %v type %v len %v",
				msg.Sequence(), msg.Type(), len(msg))
			response, err := process(wsConn.ClientId, msg)
			// log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS server process response", err, response)
			if err != nil || response == nil {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Errorf("WS server process error seq %v type %v len %v",
					msg.Sequence(), msg.Type(), len(msg))
				return
			}
			if response.Type() != msgNoResponse {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Infof("WS server process response seq %v type %v len %v",
					response.Sequence(), response.Type(), len(response))
				_, err := wsConn.Write(response)
				if err != nil {
					log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server error in response")
					return
				}
			}
		}
	}

}

var upgrader = websocket.Upgrader{}

func WebSocketHandler(handler WSServer) http.HandlerFunc {

	// Reset the map - restart many times in testing
	webSocketMap = make(map[string]*WSConn, 128)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var err error
		// device.DeviceId = deviceId
		wsConn := &WSConn{}
		wsConn.RemoteIP = net.ParseIP(base.HTTPGetSrcIP(r))
		log.WithFields(log.Fields{"public_ip": wsConn.RemoteIP}).Debug("WS server new websocket")

		wsConn.c, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("WS server upgrade:", err)
			base.SendHttpSpxError(w, http.StatusBadRequest, base.ErrorJWTInvalid.AddError(err))
			return
		}

		wsConn.callback = handler
		wsConn.readChannel = make(chan WSMsg, 16)

		// First message must be the device registration
		// wait for it
		//
		// log.Debug("WS Server authentication read")
		msg, err := wsConn.read()
		if err != nil || msg.Type() != msgAuthentication {
			log.Error("WS server didn't get auth msg: ", err, msg)
			wsConn.Close()
			return
		}

		token := ""
		// var id string
		// log.Info("WS Server authentication decode")
		err = msg.Decode(&token, &wsConn.ClientId)
		if err != nil {
			log.Errorf("WS server could not decode first message: %s %s", err, msg)
			wsConn.Close()
			return
		}

		// send OK response back
		msg, err = EncodeResponse(msg, nil)
		if err != nil {
			log.Error("WS server could not encode auth ack ", err)
			wsConn.Close()
			return
		}

		// log.Info("Server authentication response")
		_, err = wsConn.Write(msg)
		if err != nil {
			log.Error("WS server could not respond: ", err)
			wsConn.Close()
			return
		}

		// Add WS to active list
		// Close existing stale socket first
		wsMutex.Lock()
		if ws, ok := webSocketMap[wsConn.ClientId]; ok == true {
			log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Warn("WS server closing duplicated client id")
			ws.Close()
		}
		webSocketMap[wsConn.ClientId] = wsConn
		wsMutex.Unlock()

		log.WithFields(log.Fields{"clientID": wsConn.ClientId, "public_ip": wsConn.RemoteIP}).Info("WS server new websocket connection")
		handler.Accept(wsConn.ClientId)

		// Create a goroutine to read each websocket. Not very efficient for high volume
		go wsConn.serverReaderLoop(handler.Process)

	})
}

func serverPingLoop() {

	for {
		time.Sleep(pingPeriod)

		wsMutex.Lock()
		table := make([]*WSConn, len(webSocketMap))
		var i int
		for _, value := range webSocketMap {
			table[i] = value
			i++
		}
		wsMutex.Unlock()

		var conn *WSConn
		for i := range table {
			conn = table[i]

			log.WithFields(log.Fields{"clientID": conn.ClientId}).Info("WS server pinging")
			conn.writeMutex.Lock()
			err := conn.c.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait))
			conn.writeMutex.Unlock()

			if err != nil {
				log.Println("WS server ping failed", err)
				conn.serverClose()
			}
		}
	}
}

func GetWebSocketByClientId(clientId string) (wsConn *WSConn) {
	wsMutex.Lock()
	wsConn, ok := webSocketMap[clientId]
	wsMutex.Unlock()
	if !ok {
		log.Warnf("CMD cannot find websocket for deviceID %s", clientId)
		log.Info("websocketMap ", webSocketMap)
		return nil
	}
	return wsConn
}

func GetWebSocketByRemoteIP(ip net.IP) (wsConn *WSConn) {
	wsMutex.Lock()
	defer wsMutex.Unlock()

	for _, wsConn := range webSocketMap {
		if wsConn.RemoteIP.Equal(ip) {
			return wsConn
		}
	}
	log.Warnf("CMD cannot find websocket for remote IP %s", ip)
	log.Info("websocketMap ", webSocketMap)
	return nil
}
