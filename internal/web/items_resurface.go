package web

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

const itemResurfaceInterval = 60 * time.Second

func (a *App) startItemResurfacer() {
	if a == nil || a.shutdownCtx == nil {
		return
	}
	a.workerWG.Add(1)
	go func() {
		defer a.workerWG.Done()
		a.runItemResurfacer(a.shutdownCtx, itemResurfaceInterval)
	}()
}

func (a *App) runItemResurfacer(ctx context.Context, interval time.Duration) {
	if a == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if _, err := a.resurfaceDueItems(now); err != nil {
				log.Printf("items resurfacer: %v", err)
			}
		}
	}
}

func (a *App) resurfaceDueItems(now time.Time) (int, error) {
	if a == nil || a.store == nil {
		return 0, nil
	}
	count, err := a.store.ResurfaceDueItems(now)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		a.broadcastItemsResurfaced(count)
	}
	return count, nil
}

func (a *App) broadcastItemsResurfaced(count int) {
	if a == nil || count <= 0 {
		return
	}
	encoded, err := json.Marshal(map[string]interface{}{
		"type":  "items_resurfaced",
		"count": count,
	})
	if err != nil {
		return
	}
	a.hub.forEachChatConn(func(conn *chatWSConn) {
		_ = conn.writeText(encoded)
	})
}
