package ws

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

var (
	wsMutex      sync.Mutex
	webSocketMap map[string]*WSConn = make(map[string]*WSConn, 128)
	// How often do ping
	pingPeriod = 30 * time.Second

	LogAll bool
)

var upgrader = websocket.Upgrader{}

func WebSocketHandler(handler WSServer) http.HandlerFunc {

	// Reset the map - restart many times in testing
	webSocketMap = make(map[string]*WSConn, 128)
	go serverPingLoop()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var err error
		// device.DeviceId = deviceId
		wsConn := &WSConn{}
		wsConn.RemoteIP = net.ParseIP(httpGetSrcIP(r))
		if LogAll {
			log.WithFields(log.Fields{"public_ip": wsConn.RemoteIP}).Debug("WS server new websocket")
		}

		wsConn.c, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("WS server upgrade:", err)
			sendHttpSpxError(w, http.StatusBadRequest, ErrorJWTInvalid.AddError(err))
			return
		}

		wsConn.callback = handler
		wsConn.readChannel = make(chan WSMsg, 16)

		// First message must be the device registration
		// wait for it
		//
		msg, err := wsConn.read()
		if err != nil || msg.Type() != msgAuthentication {
			log.Error("WS server didn't get auth msg: ", err, msg)
			wsConn.Close()
			return
		}

		token := ""
		err = msg.Decode(&token, &wsConn.ClientId)
		if err != nil {
			log.Errorf("WS server could not decode first message: %s %s", err, msg)
			wsConn.Close()
			return
		}

		log.WithFields(log.Fields{"clientID": wsConn.ClientId, "public_ip": wsConn.RemoteIP}).Info("WS server new websocket connection")

		// Add WS to active list
		// Close existing stale socket first
		wsMutex.Lock()
		if ws, ok := webSocketMap[wsConn.ClientId]; ok == true {
			if LogAll {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS server closing duplicated client id")
			}

			wsMutex.Unlock()
			ws.serverClose()
			wsMutex.Lock()
			// clear map and close socket to cause reader to exit and cleanup
			// delete(webSocketMap, wsConn.ClientId)
			// ws.c.Close()
		}
		webSocketMap[wsConn.ClientId] = wsConn
		wsMutex.Unlock()

		if err = handler.Accept(wsConn); err != nil {
			log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server accept failed", err)
			wsConn.closing = true
			wsConn.c.Close()
			delete(webSocketMap, wsConn.ClientId)
			return
		}

		// send OK response back and the remote IP
		msg, err = EncodeResponse(msg, wsConn.RemoteIP)
		if err != nil {
			log.Error("WS server could not encode auth ack ", err)
			wsConn.serverClose()
			return
		}

		// log.Info("Server authentication response")
		_, err = wsConn.Write(msg)
		if err != nil {
			log.Error("WS server could not respond: ", err)
			wsConn.serverClose()
			return
		}

		// Create a goroutine to read each websocket. Not very efficient for high volume
		go wsConn.serverReaderLoop(handler.Process)
	})
}

// serverClose is called by the background reader or ping goroutine when the ws fails or is closed.
func (wsConn *WSConn) serverClose() {

	wsConn.writeMutex.Lock()

	// check close was not initiated normally by another goroutine.
	// if it was not closing normally, then cleanup
	if wsConn.closing {
		wsConn.writeMutex.Unlock()
		if LogAll {
			log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS server close - duplicated")
		}
		return
	}

	wsConn.closing = true
	wsConn.writeMutex.Unlock()
	log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Info("WS server close")

	// Delete entry from online table
	// ONLY if the WS has not restablished for the same device ID name
	wsMutex.Lock()
	ws := webSocketMap[wsConn.ClientId]
	if ws != wsConn {
		log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server invalid websocket map")
	}

	delete(webSocketMap, wsConn.ClientId)
	wsMutex.Unlock()

	wsConn.c.Close()
	wsConn.callback.Closed(wsConn)
}

func (wsConn *WSConn) serverReaderLoop(process func(clientId string, msg WSMsg) (response WSMsg, err error)) {
	defer log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Warn("WS server reader goroutine ended")
	defer wsConn.serverClose()

	// wsConn.c.SetReadDeadline(time.Now().Add(writeWait))
	// wsConn.c.SetReadDeadline(0)
	wsConn.lastUpdated = time.Now()
	wsConn.c.SetPongHandler(func(msg string) error {
		log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Info("PONG recv")

		wsConn.lastUpdated = time.Now()
		return nil
	})

	for {
		if LogAll {
			log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS Server read")
		}
		msg, err := wsConn.read()
		if err != nil {
			if err != ErrorClosed && !wsConn.closing { // normal closure and not closing
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

			if LogAll {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debugf("WS server got response type %v len %v", msg.Type(), len(msg))
			}
			wsConn.readChannel <- msg
			continue
		}

		switch msg.Type() {
		case msgAuthentication:
			log.Error("websocket Unexpected authentication message")

		default:
			if LogAll {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debugf("WS server process msg seq %v type %v len %v",
					msg.Sequence(), msg.Type(), len(msg))
			}
			response, err := process(wsConn.ClientId, msg)
			// log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debug("WS server process response", err, response)
			if err != nil || response == nil {
				log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Errorf("WS server process error seq %v type %v len %v",
					msg.Sequence(), msg.Type(), len(msg))
				return
			}
			if response.Type() != msgNoResponse {
				if LogAll {
					log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Debugf("WS server process response seq %v type %v len %v",
						response.Sequence(), response.Type(), len(response))
				}
				_, err := wsConn.Write(response)
				if err != nil {
					log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server error in response")
					return
				}
			}
		}
	}

}

func (wsConn *WSConn) ServerConnIsAlive() bool {
	previousUpdate := wsConn.lastUpdated

	wsConn.writeMutex.Lock()
	err := wsConn.c.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait))
	wsConn.writeMutex.Unlock()

	if err != nil {
		log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server is alive failed", err)
		return false
	}

	// 3 seconds should be enough to update ping
	time.Sleep(time.Second * 3)

	if wsConn.lastUpdated.After(previousUpdate) {
		return true
	}

	log.WithFields(log.Fields{"clientID": wsConn.ClientId}).Error("WS server conn is not responding ", err)
	return false
}

func serverPingLoop() {

	for {
		time.Sleep(pingPeriod)

		// The testing routines change the webSocketMap table during execution
		// keep it in the stack
		myTable := webSocketMap

		wsMutex.Lock()
		table := make([]*WSConn, len(myTable))
		var i int
		for _, value := range myTable {
			table[i] = value
			i++
		}
		wsMutex.Unlock()

		deadline := time.Now().Add(pingPeriod * 2 * -1)

		for i := range table {
			conn := table[i]

			conn.writeMutex.Lock()
			err := conn.c.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait))
			conn.writeMutex.Unlock()

			if err != nil {
				log.WithFields(log.Fields{"clientID": conn.ClientId}).Error("WS server ping failed ", err)
				conn.serverClose() // Close and wakeup the reader goroutine to handle the error
				continue
			}

			if conn.lastUpdated.Before(deadline) {
				log.WithFields(log.Fields{"clientID": conn.ClientId}).Error("WS server pong timeout - closing ")
				conn.serverClose() // Close and wakeup the reader goroutine to handle the error
				continue
			}
		}
	}
}

func GetWebSocketByClientId(clientId string) (wsConn *WSConn) {
	wsMutex.Lock()
	wsConn, ok := webSocketMap[clientId]
	wsMutex.Unlock()
	if !ok {
		if LogAll {
			log.Debugf("CMD cannot find websocket for deviceID %s map=%+v", clientId, webSocketMap)
		}
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

		// Return loopback wsConn if this is the local api server
		if (wsConn.RemoteIP.Equal(net.IPv6loopback) || wsConn.RemoteIP.Equal(net.IPv4(127, 0, 0, 1))) && len(webSocketMap) == 1 {
			if LogAll {
				log.Debugf("CMD found localhost websocket for ip %s ", ip)
			}
			return wsConn
		}
	}
	if LogAll {
		log.Debugf("CMD cannot find websocket for remote IP %s sockedMap=%+v", ip, webSocketMap)
	}

	return nil
}

func httpGetSrcIP(req *http.Request) string {

	ip, port, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return ""
	}

	userIP := net.ParseIP(ip)
	if userIP == nil {
		return ""
	}

	// This will only be defined when site is accessed via non-anonymous proxy
	// and takes precedence over RemoteAddr
	// Header.Get is case-insensitive
	// This is a list like "192.168.0.1, 201.123.12.8"
	ipList := req.Header.Get("X-Forwarded-For")
	if LogAll {
		log.Debugf("controller IP %s port %v  x-forwarded-for %s", ip, port, ipList)
	}

	if ipList != "" {
		tmp := strings.Split(ipList, ",")
		if len(tmp) > 0 {
			forward := strings.TrimSpace(tmp[len(tmp)-1]) // get last IP in list
			if net.ParseIP(forward) != nil {
				ip = forward
			}
		}
	}

	return ip
}

func sendHttpSpxError(w http.ResponseWriter, httpStatusCode int, err error) {
	var spxError SpxError

	spxError, ok := err.(SpxError)
	if ok == false {
		spxError = SpxError{Code: ErrorInternal.Code, Text: "unknown error", Detail: err.Error()}
	}

	w.WriteHeader(httpStatusCode)
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")

	if LogAll {
		log.WithFields(log.Fields{"code": spxError.Code, "detail": spxError.Detail, "text": spxError.Text}).Debug("SendHttpSpxError")
	}

	err = json.NewEncoder(w).Encode(spxError)
	if err != nil {
		log.Error("Failed to return SpxError struct", err)
	}
}
