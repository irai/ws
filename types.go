package ws

import (
	"bytes"
	"encoding/gob"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"net"
	"net/url"
	"sync"
	"time"
)

const (
	// Time allowed to write a message to the peer.
	writeWait   = 5 * time.Second
	readTimeout = 5 * time.Second

	// Maximum message size allowed from peer.
	maxMessageSize = 8192

	// Time to wait before force close on connection.
	closeGracePeriod = 1 * time.Second

	// Reserved msg types & sequence
	msgNoResponse     = 0
	msgSuccess        = 1
	msgAuthentication = 2
	msgReserved       = 15

	MsgClient = 16

	// First bit is used indicate response msg
	msgSeqNew      = 0x00
	msgSeqResponse = 0x80
)

var (
	NoResponse = WSMsg{0x00, msgNoResponse}
)

type WSServer interface {
	Accept(clientId string) error
	// Process(msg *WSMsg) error
	WSClient
}

type WSClient interface {
	Process(clientId string, msg WSMsg) (response WSMsg, err error)
	Closed()
}

type WSConn struct {
	c          *websocket.Conn
	url        url.URL // connection url; used for restablishing connection
	RemoteIP   net.IP  // store the client public IP on the server side
	ClientId   string  // store the device id on the server side
	writeMutex sync.Mutex
	readMutex  sync.Mutex
	// responseChannel chan WSMsg
	readChannel chan WSMsg
	callback    WSClient
	msgSeq      uint8
}

// WSMsg is a slice, so pass it by value to all
type WSMsg []byte

func (w WSMsg) Sequence() uint8       { return uint8(w[0]) & 0x7f }
func (w WSMsg) Type() uint8           { return uint8(w[1]) }
func (w WSMsg) Data() []byte          { return w[2:] }
func (w WSMsg) setSequence(seq uint8) { w[0] = seq }
func (w WSMsg) IsResponse() bool {
	if w[0]&msgSeqResponse == msgSeqResponse {
		return true
	} else {
		return false
	}
}

func Encode(msgType uint8, data interface{}) (msg WSMsg, err error) {
	var buf bytes.Buffer
	return encode(buf, msgSeqNew, msgType, data)
}

//EncodeResponse will reuse previous message
func EncodeResponse(old WSMsg, data interface{}) (msg WSMsg, err error) {
	buf := bytes.NewBuffer(old)
	buf.Reset()
	return encode(*buf, old.Sequence()|msgSeqResponse, old.Type(), data)
}

func encode(buf bytes.Buffer, seq uint8, msgType uint8, data interface{}) (msg WSMsg, err error) {
	buf.WriteByte(seq)
	buf.WriteByte(msgType)

	if data != nil {
		enc := gob.NewEncoder(&buf)
		err = enc.Encode(data)
		if err != nil {
			log.Error("WS encode error:", err)
			return nil, err
		}
	}

	msg = WSMsg(buf.Bytes())
	return

}

func (w WSMsg) Decode(data interface{}) error {

	dec := gob.NewDecoder(bytes.NewBuffer(w[2:]))
	err := dec.Decode(data)
	if err != nil {
		log.Error("WS decode error: ", err)
		return err
	}
	return nil
}
