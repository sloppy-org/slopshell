package web

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type chatWSConn struct {
	conn        *websocket.Conn
	writeMu     sync.Mutex
	sttMu       sync.Mutex
	sttActive   bool
	sttMimeType string
	sttBuf      []byte
	sttStarted  time.Time
}

func newChatWSConn(conn *websocket.Conn) *chatWSConn {
	return &chatWSConn{conn: conn}
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
