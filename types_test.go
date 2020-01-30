package ws

import (
	"net"
	"testing"

	log "github.com/sirupsen/logrus"
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

	msg, err := Encode(5, &msgToken, &data1)
	if err != nil {
		log.Fatal("cannot encode", err)
	}

	// redo to make sure all buffers are different
	msg2, err := Encode(8, &msgToken, &data1)
	if err != nil {
		log.Fatal("cannot encode", err)
	}

	token := ""
	err = msg.Decode(&token, &data2)
	if err != nil {
		log.Fatal("cannot reload", err)
	}

	if !data1.RemoteIP.Equal(data2.RemoteIP) ||
		token != msgToken ||
		msg.Sequence() != data2.msgSeq ||
		data1.ClientId != data2.ClientId ||
		msg.Type() != 5 || msg2.Type() != 8 {
		log.Fatal("values don't match ")

	}

}

func Test_StringEncodeDecode(t *testing.T) {

	var secret string = "mysecret"
	var a string = "string type"
	var b string
	msg, err := Encode(5, &secret, &a)
	if err != nil {
		log.Fatal("cannot encode", err)
	}

	token := ""
	err = msg.Decode(&token, &b)
	if err != nil {
		log.Fatal("cannot reload", err)
	}

	if a != b || token != secret {
		log.Fatal("values don't match ")
	}

}

func TestEncodeBytes(t *testing.T) {
	type device struct {
		Name   string
		Device string
	}
	tests := []struct {
		name    string
		msgType uint8
		token   string
		data    []byte
		wantMsg WSMsg
		wantErr bool
	}{
		{name: "enc1", msgType: 2, wantErr: false, token: "mytoken", data: []byte(`{"device": "mydevice"}`)},
		{name: "enc2", msgType: 2, wantErr: false, token: "mytoken2", data: nil},
		{name: "enc3", msgType: 2, wantErr: false, token: "mytoken", data: []byte(`{}`)},
		{name: "enc4", msgType: 2, wantErr: true, token: "mytoken", data: []byte(`{"device": 4}`)},
		{name: "enc5", msgType: 2, wantErr: true, token: "mytoken", data: []byte(`{"device:" ,,,,,,, }`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMsg, err := EncodeBytes(tt.msgType, &tt.token, tt.data)
			// fmt.Println("msg in =", gotMsg)
			if err != nil {
				t.Errorf("Encode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			token := ""
			device := device{}
			err = gotMsg.Decode(&token, &device)
			if (tt.wantErr && err == nil) || (!tt.wantErr && err != nil) || token != tt.token {
				t.Errorf("Decode() err = %v got = %v, token %v", err, device, token)
			}
		})
	}
}
