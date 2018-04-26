package ws

import (
	log "github.com/sirupsen/logrus"
	"net/url"
	// "net/url"
	"spinifex/base"
	"testing"
	"time"
)

// TestServerWSHandler implement the handler interface
type TestClientWSHandler struct {
	clientId string
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

func (h TestClientWSHandler) Closed(wsConn *WSConn) {
	countClosed++
	log.Infof("WS client closed %d", countClosed)
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

	conn := dial(t, *s.url, "client123")
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

func Test_ClientDialDuplicated(t *testing.T) {
	s := newServer(t)
	defer s.Close()

	countClosed = 0

	AutoRedial = false
	defer func() { AutoRedial = true }()

	conn := dial(t, *s.url, "client123")
	defer conn.Close()

	conn2 := dial(t, *s.url, "client123")
	defer conn2.Close()

	conn3 := dial(t, *s.url, "client123")
	defer conn3.Close()

	time.Sleep(time.Second * 2)

	if countClosed != 2 {
		t.Fatal("failed to close ws ", countClosed)
	}
}

func Test_ClientRedial(t *testing.T) {
	s := newServer(t)
	defer s.Close()

	conn := dial(t, *s.url, "client123")
	defer conn.Close()
	conn2 := dial(t, *s.url, "client1234")
	defer conn2.Close()

	in := simpleMsg{N: 101, Msg: "conn 2 first msg"}
	out := simpleAnswer{}
	err := conn.RPC(testEmptyResponse, &msgToken, &in, nil)
	if err != nil {
		t.Fatal("cannot rpc", err)
	}

	err = conn.RPC(testTimeout, &msgToken, &in, &out)
	if err == nil || err != base.ErrorTimeout {
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

}
