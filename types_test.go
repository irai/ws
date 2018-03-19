package ws

import (
	log "github.com/sirupsen/logrus"
	"net"
	"testing"
)

var (
	data1 = WSConn{
		// url: URL{}             url.URL // connection url; used for restablishing connection
		RemoteIP: net.ParseIP("192.168.1.1"),
		ClientId: "testclient",
	}
	data2 = WSConn{}
)

func Test_EncodeDecode(t *testing.T) {

	msg, err := Encode(5, &data1)
	if err != nil {
		log.Fatal("cannot encode", err)
	}

	// redo to make sure all buffers are different
	msg2, err := Encode(8, &data1)
	if err != nil {
		log.Fatal("cannot encode", err)
	}

	err = msg.Decode(&data2)
	if err != nil {
		log.Fatal("cannot reload", err)
	}

	if !data1.RemoteIP.Equal(data2.RemoteIP) ||
		msg.Sequence() != data2.msgSeq ||
		data1.ClientId != data2.ClientId ||
		msg.Type() != 5 || msg2.Type() != 8 {
		log.Fatal("values don't match ", data1, data2)

	}

}

func Test_StringEncodeDecode(t *testing.T) {

	var a string = "string type"
	var b string
	msg, err := Encode(5, &a)
	if err != nil {
		log.Fatal("cannot encode", err)
	}

	err = msg.Decode(&b)
	if err != nil {
		log.Fatal("cannot reload", err)
	}

	if a != b {
		log.Fatal("values don't match ", data1, data2)
	}

}
