package ws

import (
	log "github.com/sirupsen/logrus"
	"net/url"
	// "net/url"
	"testing"
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

func (h TestClientWSHandler) Closed() { log.Info("WS client socket closed ") }

func dial(t *testing.T, u url.URL, clientId string) *WSConn {

	conn, err := WebSocketDial(u, clientId, TestClientWSHandler{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return conn
}

func Test_ClientDial(t *testing.T) {
	s := newServer(t)
	defer s.Close()

	conn := dial(t, *s.url, "client123")
	defer conn.Close()

	msg, err := Encode(testEmptyResponse, &data1)
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
