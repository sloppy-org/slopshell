package web

import (
	"context"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/serve"
)

type tunnelRegistry struct {
	mu             sync.Mutex
	ports          map[string]int
	remoteCanvas   map[string]*websocket.Conn
	relayCancel    map[string]context.CancelFunc
	projectApps    map[string]*serve.App
	projectStop    map[string]context.CancelFunc
	localApp       *serve.App
	localAppCancel context.CancelFunc
}

func newTunnelRegistry() *tunnelRegistry {
	return &tunnelRegistry{
		ports:        map[string]int{},
		remoteCanvas: map[string]*websocket.Conn{},
		relayCancel:  map[string]context.CancelFunc{},
		projectApps:  map[string]*serve.App{},
		projectStop:  map[string]context.CancelFunc{},
	}
}

func (t *tunnelRegistry) getPort(sessionID string) (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	port, ok := t.ports[sessionID]
	return port, ok
}

func (t *tunnelRegistry) setPort(sessionID string, port int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ports[sessionID] = port
}

func (t *tunnelRegistry) hasPort(sessionID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.ports[sessionID]
	return ok
}

func (t *tunnelRegistry) getRemoteCanvas(sessionID string) *websocket.Conn {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.remoteCanvas[sessionID]
}

func (t *tunnelRegistry) setRemoteCanvas(sessionID string, ws *websocket.Conn) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.remoteCanvas[sessionID] = ws
}

func (t *tunnelRegistry) deleteRemoteCanvas(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if rc := t.remoteCanvas[sessionID]; rc != nil {
		_ = rc.Close()
	}
	delete(t.remoteCanvas, sessionID)
}

func (t *tunnelRegistry) replaceRelayCancel(sessionID string) context.Context {
	t.mu.Lock()
	if cancel := t.relayCancel[sessionID]; cancel != nil {
		cancel()
		delete(t.relayCancel, sessionID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.relayCancel[sessionID] = cancel
	t.mu.Unlock()
	return ctx
}

func (t *tunnelRegistry) deleteRelayCancel(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.relayCancel, sessionID)
}

func (t *tunnelRegistry) setProjectServe(sessionID string, app *serve.App, cancel context.CancelFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.projectApps[sessionID] = app
	t.projectStop[sessionID] = cancel
}

func (t *tunnelRegistry) setLocalServe(app *serve.App, cancel context.CancelFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.localApp = app
	t.localAppCancel = cancel
}

func (t *tunnelRegistry) shutdown(ctx context.Context) {
	t.mu.Lock()
	for _, cancel := range t.relayCancel {
		cancel()
	}
	for _, ws := range t.remoteCanvas {
		_ = ws.Close()
	}
	projectStops := make(map[string]context.CancelFunc, len(t.projectStop))
	for sid, cancel := range t.projectStop {
		projectStops[sid] = cancel
	}
	projectApps := make(map[string]*serve.App, len(t.projectApps))
	for sid, app := range t.projectApps {
		projectApps[sid] = app
	}
	localApp := t.localApp
	localCancel := t.localAppCancel
	t.mu.Unlock()

	for _, cancel := range projectStops {
		cancel()
	}
	for _, app := range projectApps {
		if app != nil {
			_ = app.Stop(ctx)
		}
	}
	if localApp != nil {
		_ = localApp.Stop(ctx)
	}
	if localCancel != nil {
		localCancel()
	}
}
