package ws

import (
	log "github.com/sirupsen/logrus"
	"net/http/httptest"
	"net/url"
	// "net/url"
	"fmt"
	"spinifex/base"
	"strings"
	"testing"
	"time"
)

type simpleMsg struct {
	Msg string
	N   int
}
type simpleAnswer struct {
	Msg string
	N   int
}

const (
	testBounce = MsgClient + iota
	testTimeout
	testNoResponse
	testAbruptClose
	testEmptyResponse
)

// type cstHandler struct{ *testing.T }

type wsServer struct {
	*httptest.Server
	url *url.URL
}

var (
	serverHandler *wsServer
)

// TestServerWSHandler implement the handler interface
type TestServerWSHandler struct {
	clientId string
}

func (h TestServerWSHandler) Process(clientId string, msg WSMsg) (response WSMsg, err error) {
	log.Info("got new msg for ", clientId)
	switch msg.Type() {
	case testEmptyResponse:
		response, err = EncodeResponse(msg, nil)
		return response, nil

	case testAbruptClose:
		return NoResponse, base.ErrorClosed

	case testTimeout, testNoResponse:
		return NoResponse, nil

	}
	return NoResponse, nil
}

func (h TestServerWSHandler) Accept(wsConn *WSConn) error {
	log.Info("WS server running accept ", wsConn.ClientId)
	return nil
}

func (h TestServerWSHandler) Closed() { log.Info("WS server socket closed ") }

func newServer(t *testing.T) *wsServer {
	var s wsServer
	s.Server = httptest.NewServer(WebSocketHandler(&TestServerWSHandler{clientId: "server123"}))
	u := "ws" + strings.TrimPrefix(s.Server.URL, "http") // make WS protocol
	s.url, _ = url.Parse(u + "/ws/")
	return &s
}

func setupServer(t *testing.T) {
	if serverHandler == nil {
		serverHandler = newServer(t)
	}
	// Clean up websocket map
	webSocketMap = make(map[string]*WSConn, 128)
}

func Test_ServerConn(t *testing.T) {
	// s := newServer(t)
	// defer s.Close()
	setupServer(t)

	conn1 := dial(t, *serverHandler.url, "client1")
	defer conn1.Close()

	conn2 := dial(t, *serverHandler.url, "client2")
	// conn3 := dial(t, *serverHandler.url, "client3")

	in := simpleMsg{N: 100, Msg: "conn 1 first msg"}
	// out := simpleAnswer{}

	err := conn1.RPC(testEmptyResponse, &msgToken, &in, nil)
	if err != nil {
		t.Fatal("cannot rpc", err)
	}

	in2 := simpleMsg{N: 101, Msg: "conn 2 first msg"}
	out2 := simpleAnswer{}
	err = conn2.RPC(testTimeout, &msgToken, &in2, &out2)
	if err == nil || err != base.ErrorTimeout {
		t.Fatal("cannot rpc", err)
	}

	conn2.Close()

	// resend in 1
	err = conn1.RPC(testEmptyResponse, &msgToken, &in, nil)
	if err != nil {
		t.Fatal("cannot rpc", err)
	}

}

func Test_ServerAccept(t *testing.T) {
	// s := newServer(t)
	// defer s.Close()
	setupServer(t)

	total := 64

	var table []*WSConn
	for i := 0; i < total; i++ {
		n := i
		go func() {
			conn := dial(t, *serverHandler.url, fmt.Sprintf("client %d", n))
			table = append(table, conn)
		}()
	}

	time.Sleep(time.Second * 3)
	if len(webSocketMap) != total {
		t.Fatal("wrong total", len(webSocketMap))
	}

	for i := range table {
		go table[i].Close()
	}

	time.Sleep(time.Second * 2)

	if len(webSocketMap) != 0 {
		t.Fatal("wrong end total", len(webSocketMap))
	}
}

func Test_ServerPing(t *testing.T) {
	setupServer(t)

	pingPeriod = 500 * time.Millisecond

	conn1 := dial(t, *serverHandler.url, "client1")
	defer conn1.Close()

	in := simpleMsg{N: 100, Msg: "conn 1 first msg"}
	// out := simpleAnswer{}

	err := conn1.RPC(testEmptyResponse, &msgToken, &in, nil)
	if err != nil {
		t.Fatal("cannot rpc", err)
	}

	time.Sleep(time.Second * 1)
	if len(webSocketMap) != 1 {
		t.Fatal("wrong total", len(webSocketMap))
	}

	conn1.c.Close() // close underlying socket - client will redial
	time.Sleep(time.Second * 1)
	if len(webSocketMap) != 1 {
		t.Fatal("wrong end total", len(webSocketMap))
	}

}

func Test_ServerDupClient(t *testing.T) {
	setupServer(t)

	conn1 := dial(t, *serverHandler.url, "client1")
	defer conn1.Close()
	conn2 := dial(t, *serverHandler.url, "client2")
	defer conn2.Close()

	if len(webSocketMap) != 2 {
		t.Fatal("wrong total", len(webSocketMap))
	}

	conn3 := dial(t, *serverHandler.url, "client1")
	defer conn3.Close()

	if len(webSocketMap) != 2 {
		t.Fatal("wrong total", len(webSocketMap))
	}
}
