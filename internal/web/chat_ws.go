package web

import (
	"sync"

	"github.com/gorilla/websocket"
)

type chatWSConn struct {
	conn        *websocket.Conn
	writeMu     sync.Mutex
	sttMu       sync.Mutex
	sttActive   bool
	sttMimeType string
	sttBuf      []byte
	ttsMu       sync.Mutex
	ttsNextSeq  int64
	ttsNextEmit int64
	ttsPending  map[int64]ttsOrderedResult
}

func newChatWSConn(conn *websocket.Conn) *chatWSConn {
	return &chatWSConn{
		conn:       conn,
		ttsPending: map[int64]ttsOrderedResult{},
	}
}

func (c *chatWSConn) writeJSON(v interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

func (c *chatWSConn) writeText(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *chatWSConn) writeBinary(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteMessage(websocket.BinaryMessage, data)
}

type ttsOrderedResult struct {
	seq   int64
	audio []byte
	err   string
}

func (c *chatWSConn) reserveTTSSeq() int64 {
	c.ttsMu.Lock()
	defer c.ttsMu.Unlock()
	seq := c.ttsNextSeq
	c.ttsNextSeq++
	return seq
}

func (c *chatWSConn) completeTTSSeq(seq int64, audio []byte, errMsg string) []ttsOrderedResult {
	c.ttsMu.Lock()
	defer c.ttsMu.Unlock()

	c.ttsPending[seq] = ttsOrderedResult{
		seq:   seq,
		audio: audio,
		err:   errMsg,
	}

	ready := make([]ttsOrderedResult, 0, 2)
	for {
		result, ok := c.ttsPending[c.ttsNextEmit]
		if !ok {
			break
		}
		delete(c.ttsPending, c.ttsNextEmit)
		c.ttsNextEmit++
		ready = append(ready, result)
	}
	return ready
}
