package web

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

func parseInlineSomedayIntent(text string) *SystemAction {
	normalized := normalizeItemCommandText(text)
	switch normalized {
	case "review my someday list", "review someday list", "what's in someday", "what is in someday", "zeige irgendwann", "was ist auf irgendwann":
		return &SystemAction{Action: "review_someday", Params: map[string]interface{}{}}
	case "someday", "not now", "maybe later", "defer to someday", "defer this to someday", "defer it to someday", "irgendwann", "nicht jetzt", "vielleicht spaeter":
		return &SystemAction{Action: "triage_someday", Params: map[string]interface{}{}}
	case "bring this back", "make this active", "move this to inbox", "move it to inbox", "move this back to inbox", "move this back to the inbox", "move this mail back to the inbox", "hol das zurueck", "mach das wieder aktiv", "verschiebe das in den posteingang":
		return &SystemAction{Action: "promote_someday", Params: map[string]interface{}{}}
	case "turn off someday reminders", "disable someday reminders", "disable someday review reminders", "schalte irgendwann erinnerungen aus":
		return &SystemAction{Action: "toggle_someday_review_nudge", Params: map[string]interface{}{"enabled": false}}
	case "turn on someday reminders", "enable someday reminders", "enable someday review reminders", "schalte irgendwann erinnerungen an":
		return &SystemAction{Action: "toggle_someday_review_nudge", Params: map[string]interface{}{"enabled": true}}
	default:
		return nil
	}
}

func systemActionEnabled(params map[string]interface{}) (bool, bool) {
	if params == nil {
		return false, false
	}
	value, ok := params["enabled"]
	if !ok {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on", "enabled":
			return true, true
		case "0", "false", "no", "off", "disabled":
			return false, true
		}
	}
	return false, false
}

func (a *App) resolveFocusedItemTarget(session store.ChatSession, action *SystemAction) (store.Item, error) {
	if action != nil {
		if itemID := systemActionItemID(action.Params); itemID > 0 {
			return a.store.GetItem(itemID)
		}
	}
	project, err := a.systemActionTargetProject(session)
	if err != nil {
		return store.Item{}, err
	}
	if item, err := a.resolveCanvasConversationItem(project); err == nil {
		return item, nil
	} else if !isNoRows(err) {
		return store.Item{}, err
	}
	return store.Item{}, errors.New("open the item on canvas first or provide item_id")
}

func (a *App) executeSomedayAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "review_someday":
		items, err := a.store.ListSomedayItems()
		if err != nil {
			return "", nil, err
		}
		count := len(items)
		if count == 0 {
			return "Opened someday list. There are no someday items right now.", map[string]interface{}{
				"type":  "show_item_sidebar_view",
				"view":  store.ItemStateSomeday,
				"count": 0,
			}, nil
		}
		return fmt.Sprintf("Opened someday list with %d item(s).", count), map[string]interface{}{
			"type":  "show_item_sidebar_view",
			"view":  store.ItemStateSomeday,
			"count": count,
		}, nil
	case "toggle_someday_review_nudge":
		enabled, ok := systemActionEnabled(action.Params)
		if !ok {
			return "", nil, errors.New("enabled flag is required")
		}
		state := "disabled"
		if enabled {
			state = "enabled"
		}
		return fmt.Sprintf("Someday review reminders %s.", state), map[string]interface{}{
			"type":    "set_someday_review_nudge",
			"enabled": enabled,
		}, nil
	case "triage_someday":
		item, err := a.resolveFocusedItemTarget(session, action)
		if err != nil {
			return "", nil, err
		}
		if err := a.store.TriageItemSomeday(item.ID); err != nil {
			return "", nil, err
		}
		updated, err := a.store.GetItem(item.ID)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Moved %q to someday.", updated.Title), map[string]interface{}{
			"type":    "item_state_changed",
			"item_id": updated.ID,
			"state":   updated.State,
			"view":    store.ItemStateSomeday,
		}, nil
	case "promote_someday":
		item, err := a.resolveFocusedItemTarget(session, action)
		if err != nil {
			return "", nil, err
		}
		if err := a.syncRemoteEmailItemState(context.Background(), item, store.ItemStateInbox); err != nil {
			return "", nil, err
		}
		if err := a.store.UpdateItemState(item.ID, store.ItemStateInbox); err != nil {
			return "", nil, err
		}
		updated, err := a.store.GetItem(item.ID)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Moved %q back to inbox.", updated.Title), map[string]interface{}{
			"type":    "item_state_changed",
			"item_id": updated.ID,
			"state":   updated.State,
			"view":    store.ItemStateSomeday,
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported someday action %q", action.Action)
	}
}
