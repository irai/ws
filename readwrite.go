package ws

// #
// # NOT used
// # This should be deleted - July 2017

import (
	// "bytes"
	// "encoding/gob"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"spinifex/base"
	"time"
)

func channelIsClosed(ch <-chan WSMsg) bool {
	select {
	case <-ch:
		return true

	default:
	}
	return false
}

func (wsConn *WSConn) WaitResponse(timeout time.Duration) (msg WSMsg, err error) {
	// if channelIsClosed(wsConn.readChannel) {
	// log.Errorf("WS wait channel is closed")
	// return WSMsg{}, base.ErrorInternal
	// }
	select {
	case msg := <-wsConn.readChannel:
		return msg, nil

	case <-time.After(timeout):
		log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Error("WS wait timed out")
		// close(wsConn.readChannel)
		wsConn.c.Close() // force readerLoop to exit and redial
		return WSMsg{}, base.ErrorTimeout

		// default:
	}

	log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Error("WS wait internal")
	return WSMsg{}, base.ErrorInternal
}

func (wsConn *WSConn) read() (msg WSMsg, err error) {

	// log.Info("read")
	wsConn.readMutex.Lock()

	_, packet, err := wsConn.c.ReadMessage()

	wsConn.readMutex.Unlock()

	if err != nil {
		if e, ok := err.(*websocket.CloseError); ok && e.Code == websocket.CloseNormalClosure {
			return nil, base.ErrorClosed
		}
		return nil, err
	}

	if len(packet) < 2 {
		log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Error("WS invalid msg", packet)
		return nil, base.ErrorInvalidRequest
	}

	msg = WSMsg(packet)

	return msg, nil
}

func (wsConn *WSConn) Write(msg WSMsg) (seq uint8, err error) {
	// log.Info("write")

	// Add sequence number if this is new message
	// if msg.Sequence() == msgSeqNew {
	if !msg.IsResponse() {
		wsConn.msgSeq = (wsConn.msgSeq + 1) & 0x7f // drop the first bit
		msg.setSequence(wsConn.msgSeq)
	}

	wsConn.writeMutex.Lock()
	defer wsConn.writeMutex.Unlock()

	wsConn.c.SetWriteDeadline(time.Now().Add(writeWait))
	if err := wsConn.c.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Error("WS failed to write websocket msg: ", err)
		return 0, err
	}

	return msg.Sequence(), nil
}

func (wsConn *WSConn) RPC(msgType uint8, token *string, in interface{}, out interface{}) (err error) {
	msg, err := Encode(msgType, token, in)
	if err != nil {
		return err
	}
	response, err := wsConn.WriteAndWaitResponse(msg)
	if err != nil {
		return err
	}
	if out != nil {
		err = response.Decode(nil, out)
		if err != nil {
			return err
		}
	}
	return nil
}

func (wsConn *WSConn) WriteAndWaitResponse(msg WSMsg) (response WSMsg, err error) {
	// log.Info("Writing")
	seq, err := wsConn.Write(msg)
	if err != nil {
		return WSMsg{}, err
	}

	// log.Info("Waiting")
	response, err = wsConn.WaitResponse(readTimeout)
	if err != nil {
		// log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Error("WS waiting response ", err)
		return WSMsg{}, err
	}
	if seq != response.Sequence() {
		log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Error("WS unexpected sequence: ")
		return WSMsg{}, base.ErrorInternal

	}
	// log.WithFields(log.Fields{"clientId": wsConn.ClientId}).Info("WS received response ", response)
	return response, nil
}
