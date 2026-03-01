package web

import (
	"sync"

	"github.com/gorilla/websocket"
)

type wsHub struct {
	mu       sync.Mutex
	canvasWS map[string]map[*websocket.Conn]struct{}
	chatWS   map[string]map[*chatWSConn]struct{}
}

func newWSHub() *wsHub {
	return &wsHub{
		canvasWS: map[string]map[*websocket.Conn]struct{}{},
		chatWS:   map[string]map[*chatWSConn]struct{}{},
	}
}

func (h *wsHub) registerCanvas(sid string, ws *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.canvasWS[sid] == nil {
		h.canvasWS[sid] = map[*websocket.Conn]struct{}{}
	}
	h.canvasWS[sid][ws] = struct{}{}
}

func (h *wsHub) unregisterCanvas(sid string, ws *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.canvasWS[sid]; set != nil {
		delete(set, ws)
	}
}

func (h *wsHub) canvasClients(sid string) []*websocket.Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	clients := make([]*websocket.Conn, 0, len(h.canvasWS[sid]))
	for ws := range h.canvasWS[sid] {
		clients = append(clients, ws)
	}
	return clients
}

func (h *wsHub) registerChat(sessionID string, conn *chatWSConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.chatWS[sessionID] == nil {
		h.chatWS[sessionID] = map[*chatWSConn]struct{}{}
	}
	h.chatWS[sessionID][conn] = struct{}{}
}

func (h *wsHub) unregisterChat(sessionID string, conn *chatWSConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.chatWS[sessionID]; set != nil {
		delete(set, conn)
	}
}

func (h *wsHub) chatClients(sessionID string) []*chatWSConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	clients := make([]*chatWSConn, 0, len(h.chatWS[sessionID]))
	for conn := range h.chatWS[sessionID] {
		clients = append(clients, conn)
	}
	return clients
}

func (h *wsHub) forEachChatConn(fn func(*chatWSConn)) {
	h.mu.Lock()
	all := make([]*chatWSConn, 0)
	for _, set := range h.chatWS {
		for conn := range set {
			all = append(all, conn)
		}
	}
	h.mu.Unlock()
	for _, conn := range all {
		fn(conn)
	}
}

func (h *wsHub) closeAllChat() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, set := range h.chatWS {
		for conn := range set {
			_ = conn.conn.Close()
		}
	}
}
