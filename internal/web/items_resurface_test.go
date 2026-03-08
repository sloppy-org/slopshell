package web

import (
	"context"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func TestItemResurfacerTickerMovesDueItemsBackToInbox(t *testing.T) {
	app := newAuthedTestApp(t)

	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	future := time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	dueItem, err := app.store.CreateItem("due", store.ItemOptions{
		State:        store.ItemStateWaiting,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(due) error: %v", err)
	}
	futureItem, err := app.store.CreateItem("future", store.ItemOptions{
		State:        store.ItemStateWaiting,
		VisibleAfter: &future,
	})
	if err != nil {
		t.Fatalf("CreateItem(future) error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.runItemResurfacer(ctx, 10*time.Millisecond)
	}()
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		item, err := app.store.GetItem(dueItem.ID)
		if err != nil {
			t.Fatalf("GetItem(due) error: %v", err)
		}
		if item.State == store.ItemStateInbox {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	dueItem, err = app.store.GetItem(dueItem.ID)
	if err != nil {
		t.Fatalf("GetItem(due, final) error: %v", err)
	}
	if dueItem.State != store.ItemStateInbox {
		t.Fatalf("due item state = %q, want %q", dueItem.State, store.ItemStateInbox)
	}
	futureItem, err = app.store.GetItem(futureItem.ID)
	if err != nil {
		t.Fatalf("GetItem(future, final) error: %v", err)
	}
	if futureItem.State != store.ItemStateWaiting {
		t.Fatalf("future item state = %q, want %q", futureItem.State, store.ItemStateWaiting)
	}
}

func TestItemResurfaceBroadcastsWebsocketNotification(t *testing.T) {
	app := newAuthedTestApp(t)

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	past := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)
	pastValue := past.Add(-time.Minute).Format(time.RFC3339)
	_, err = app.store.CreateItem("due", store.ItemOptions{
		State:        store.ItemStateWaiting,
		VisibleAfter: &pastValue,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	count, err := app.resurfaceDueItems(past)
	if err != nil {
		t.Fatalf("resurfaceDueItems() error: %v", err)
	}
	if count != 1 {
		t.Fatalf("resurfaceDueItems() count = %d, want 1", count)
	}

	payload := waitForWSJSONMessageType(t, clientConn, 2*time.Second, "items_resurfaced")
	if got, ok := payload["count"].(float64); !ok || int(got) != 1 {
		t.Fatalf("items_resurfaced count = %#v, want 1", payload["count"])
	}
}
