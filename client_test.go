package ws

import (
	"net/url"

	log "github.com/sirupsen/logrus"

	// "net/url"
	"testing"
	"time"
)

// TestServerWSHandler implement the handler interface
type TestClientWSHandler struct {
	clientId string
	WSClient
}

func (h TestClientWSHandler) Process(clientId string, msg WSMsg) (response WSMsg, err error) {
	log.Info("got new msg ")
	switch msg.Type() {
	case testEmptyResponse:
		response, err = EncodeResponse(msg, nil)
		return response, nil

	case testTimeout, testNoResponse:
		return NoResponse, nil

	}
	return NoResponse, nil
}

// func (h TestClientWSHandler) Accept(clientId string) error {
// log.Info("WS server running accept ", clientId)
// return nil
// }

var countClosed int
var countRedial int

func (h TestClientWSHandler) Closed(wsConn *WSConn) {
	countClosed++
	log.Infof("WS client %s nclosed %d", wsConn.ClientId, countClosed)
}

func (h TestClientWSHandler) BeforeRedial() {
	countRedial++
	log.Infof("WS client before redial count=%d", countRedial)
}

func (h TestClientWSHandler) AfterRedial(wsConn *WSConn) {
	log.Infof("WS client after redial %s count %d", wsConn.ClientId, countRedial)
}

func dial(t *testing.T, u url.URL, clientId string) *WSConn {

	conn, err := WebSocketDial(u, clientId, TestClientWSHandler{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return conn
}

var msgToken = "secrettoken"

func Test_ClientDial(t *testing.T) {
	s := newServer(t)
	defer s.Close()

	conn := dial(t, *s.url, "client123DIAL")
	defer conn.Close()

	msg, err := Encode(testEmptyResponse, &msgToken, &data1)
	if err != nil {
		t.Fatal("cannot encode", err)
	}
	response, err := conn.WriteAndWaitResponse(msg)
	if err != nil ||
		msg.Sequence() != response.Sequence() ||
		msg.Type() != response.Type() {
		t.Fatal("failed to get response", err, msg, response)
	}
	log.Info("received successful response")
}

func Test_ClientNormalClosure(t *testing.T) {
	s := newServer(t)
	defer s.Close()

	countClosed = 0

	conn := dial(t, *s.url, "client1DUP")
	conn2 := dial(t, *s.url, "client12DUP")
	conn3 := dial(t, *s.url, "client123DUP")

	conn2.Close()
	conn.Close()
	conn3.Close()

	time.Sleep(time.Second * 2)

	if countClosed != 3 {
		t.Fatal("failed to close ws ", countClosed)
	}
}

func Test_ClientDialDuplicated(t *testing.T) {
	s := newServer(t)
	defer s.Close()

	countClosed = 0

	AutoRedial = false
	defer func() { AutoRedial = true }()

	conn := dial(t, *s.url, "client123DUP")
	defer conn.Close()

	conn2 := dial(t, *s.url, "client123DUP")
	defer conn2.Close()

	conn3 := dial(t, *s.url, "client123DUP")
	defer conn3.Close()

	time.Sleep(time.Second * 2)

	if countClosed != 2 {
		t.Fatal("failed to close ws ", countClosed)
	}
}

func Test_ClientRedial(t *testing.T) {
	s := newServer(t)
	defer s.Close()

	countRedial = 0

	conn := dial(t, *s.url, "client123REDIAL")
	defer conn.Close()
	conn2 := dial(t, *s.url, "client1234REDIAL")
	defer conn2.Close()

	in := simpleMsg{N: 101, Msg: "conn 2 first msg"}
	out := simpleAnswer{}
	err := conn.RPC(testEmptyResponse, &msgToken, &in, nil)
	if err != nil {
		t.Fatal("cannot rpc", err)
	}

	err = conn.RPC(testTimeout, &msgToken, &in, &out)
	if err == nil || err != ErrorTimeout {
		t.Fatal("cannot rpc timeout", err)
	}

	n := 3
	i := 0
	for i = 0; i < n; i++ {
		time.Sleep(time.Second * 1)
		log.Info("msg ", i)
		err = conn.RPC(testEmptyResponse, &msgToken, &in, nil)
		if err == nil {
			break
		}
	}
	if i >= n {
		t.Fatal("cannot redial ", err)
	}

	err = conn.RPC(testEmptyResponse, &msgToken, &in, nil)
	if err != nil {
		t.Fatal("cannot rpc2", err)
	}
	err = conn2.RPC(testEmptyResponse, &msgToken, &in, nil)
	if err != nil {
		t.Fatal("cannot rpc3", err)
	}

	time.Sleep(time.Second * 3)

	if len(webSocketMap) != 2 || countConnections != 2 || countRedial != 1 {
		t.Fatal("wrong total", len(webSocketMap), countConnections, countRedial)
	}

}

var (
	savedClientPingPeriod time.Duration
	savedServerPingPeriod time.Duration
)

func setAndSaveEnv(cPingPeriod time.Duration, serverPingPeriod time.Duration) {
	countConnections = 0
	countRedial = 0
	savedClientPingPeriod = clientPingPeriod
	savedServerPingPeriod = pingPeriod
	clientPingPeriod = cPingPeriod
	pingPeriod = serverPingPeriod
}
func resetEnv() {
	pingPeriod = savedServerPingPeriod
	clientPingPeriod = savedClientPingPeriod
}

func Test_ClientRedialPINGFailure(t *testing.T) {

	setAndSaveEnv(time.Second*2, time.Second*10)
	defer resetEnv()

	s := newServer(t)
	defer s.Close()

	conn := dial(t, *s.url, "client123PING")
	defer conn.Close()
	conn2 := dial(t, *s.url, "client1234PING")
	defer conn2.Close()

	in := simpleMsg{N: 101, Msg: "conn 2 first msg"}
	// out := simpleAnswer{}
	err := conn.RPC(testEmptyResponse, &msgToken, &in, nil)
	if err != nil {
		t.Fatal("cannot rpc", err)
	}

	n := 3
	i := 0
	for i = 0; i < n; i++ {
		time.Sleep(time.Second * 3)
		log.Info("msg ", i)
		err = conn.RPC(testEmptyResponse, &msgToken, &in, nil)
		if err == nil {
			break
		}
	}
	if i >= n {
		t.Fatal("cannot redial ", err)
	}

	err = conn.RPC(testEmptyResponse, &msgToken, &in, nil)
	if err != nil {
		t.Fatal("cannot rpc2", err)
	}
	err = conn2.RPC(testEmptyResponse, &msgToken, &in, nil)
	if err != nil {
		t.Fatal("cannot rpc3", err)
	}

	time.Sleep(time.Second * 3)

	if len(webSocketMap) != 2 || countConnections != 2 || countRedial != 2 {
		t.Fatal("wrong total", len(webSocketMap), countConnections, countRedial)
	}

}
