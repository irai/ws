package ws

import (
	"bytes"
	"encoding/json"
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
	NoResponse      = WSMsg{0x00, msgNoResponse}
	AutoRedial bool = true
)

type WSServer interface {
	Accept(wsConn *WSConn) error
	// Process(msg *WSMsg) error
	WSClient
}

type WSClient interface {
	Process(clientId string, msg WSMsg) (response WSMsg, err error)
	Closed(wsConn *WSConn)
}

type WSConn struct {
	c           *websocket.Conn
	url         url.URL // connection url; used for restablishing connection
	RemoteIP    net.IP  // store the client public IP on the server side
	ClientId    string  // store the device id on the server side
	writeMutex  sync.Mutex
	readMutex   sync.Mutex
	readChannel chan WSMsg
	callback    WSClient
	msgSeq      uint8
	closing     bool
	lastUpdated time.Time
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

func Encode(msgType uint8, token *string, data interface{}) (msg WSMsg, err error) {
	var buf bytes.Buffer
	return encode(buf, msgSeqNew, msgType, token, data)
}

//EncodeResponse will reuse previous message
func EncodeResponse(old WSMsg, data interface{}) (msg WSMsg, err error) {
	buf := bytes.NewBuffer(old)
	buf.Reset()
	return encode(*buf, old.Sequence()|msgSeqResponse, old.Type(), nil, data)
}

func encode(buf bytes.Buffer, seq uint8, msgType uint8, token *string, data interface{}) (msg WSMsg, err error) {
	if token == nil {
		var noToken = ""
		token = &noToken
	}

	buf.WriteByte(seq)
	buf.WriteByte(msgType)

	enc := json.NewEncoder(&buf)
	if err = enc.Encode(token); err != nil {
		log.Error("WS encode token error:", err)
		return nil, err
	}

	if data != nil {
		if err = enc.Encode(data); err != nil {
			log.Error("WS json encode error:", err)
			return nil, err
		}
	}

	msg = WSMsg(buf.Bytes())
	return
}

func (w WSMsg) Decode(token *string, data interface{}) error {
	empty := ""
	dec := json.NewDecoder(bytes.NewBuffer(w[2:]))
	if token == nil { // caller is not interested in token
		token = &empty
	}

	if err := dec.Decode(&token); err != nil {
		log.Error("WS decode token error: ", err)
		return err
	}

	if data != nil {
		if err := dec.Decode(data); err != nil {
			log.Error("WS data decode error: ", err)
			return err
		}
	}
	return nil
}
