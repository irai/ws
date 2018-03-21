package ws

import (
	log "github.com/sirupsen/logrus"
	// "net/url"
	"testing"
)

func Test_ReadWriteSequence(t *testing.T) {
	s := newServer(t)
	defer s.Close()

	conn := dial(t, *s.url, "testsequence")
	defer conn.Close()

	for i := 0; i < 1024; i++ {
		data := simpleMsg{N: i, Msg: "bob"}

		msg, err := Encode(testEmptyResponse, &msgToken, &data)
		if err != nil {
			t.Fatal("cannot encode", err)
		}
		log.Info("sending ", data)

		response, err := conn.WriteAndWaitResponse(msg)
		if err != nil ||
			msg.Sequence() != response.Sequence() ||
			msg.Type() != response.Type() {
			t.Fatal("failed to get response", err, msg, response)
		}
	}
}
