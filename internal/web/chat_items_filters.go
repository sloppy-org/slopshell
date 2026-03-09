package web

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

var itemFilterSourceCommands = map[string]string{
	"show todoist tasks":      store.ExternalProviderTodoist,
	"show my todoist tasks":   store.ExternalProviderTodoist,
	"show todoist items":      store.ExternalProviderTodoist,
	"zeige todoist aufgaben":  store.ExternalProviderTodoist,
	"zeige todoist items":     store.ExternalProviderTodoist,
	"show gmail messages":     store.ExternalProviderGmail,
	"show gmail items":        store.ExternalProviderGmail,
	"zeige gmail nachrichten": store.ExternalProviderGmail,
	"show imap messages":      store.ExternalProviderIMAP,
	"show imap items":         store.ExternalProviderIMAP,
	"show exchange messages":  store.ExternalProviderExchange,
	"show exchange items":     store.ExternalProviderExchange,
	"show github items":       "github",
	"show manual items":       "manual",
}

func parseInlineItemFilterIntent(text string) *SystemAction {
	normalized := normalizeItemCommandText(text)
	allSpheres := false
	switch {
	case strings.HasPrefix(normalized, "show all "):
		normalized = "show " + strings.TrimSpace(strings.TrimPrefix(normalized, "show all "))
		allSpheres = true
	case strings.HasPrefix(normalized, "open all "):
		normalized = "open " + strings.TrimSpace(strings.TrimPrefix(normalized, "open all "))
		allSpheres = true
	case strings.HasPrefix(normalized, "zeige alle "):
		normalized = "zeige " + strings.TrimSpace(strings.TrimPrefix(normalized, "zeige alle "))
		allSpheres = true
	}
	switch normalized {
	case "show inbox", "open inbox", "show my inbox", "zeige posteingang", "oeffne posteingang", "zeige meinen posteingang":
		return &SystemAction{
			Action: "show_filtered_items",
			Params: map[string]interface{}{
				"view":          store.ItemStateInbox,
				"clear_filters": true,
				"all_spheres":   allSpheres,
			},
		}
	case "show unassigned items", "show unassigned inbox items", "show items without workspace", "zeige nicht zugeordnete items", "zeige items ohne workspace":
		return &SystemAction{
			Action: "show_filtered_items",
			Params: map[string]interface{}{
				"view": store.ItemStateInbox,
				"filters": map[string]interface{}{
					"workspace_id": "null",
					"all_spheres":  allSpheres,
				},
			},
		}
	}
	if source, ok := itemFilterSourceCommands[normalized]; ok {
		return &SystemAction{
			Action: "show_filtered_items",
			Params: map[string]interface{}{
				"view": store.ItemStateInbox,
				"filters": map[string]interface{}{
					"source":      source,
					"all_spheres": allSpheres,
				},
			},
		}
	}
	return nil
}

func systemActionTruthyParam(params map[string]interface{}, key string) bool {
	value, ok := params[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}

func systemActionNestedParams(params map[string]interface{}, key string) map[string]interface{} {
	if params == nil {
		return nil
	}
	nested, _ := params[key].(map[string]interface{})
	return nested
}

func itemFilterFromActionParams(params map[string]interface{}) (store.ItemListFilter, error) {
	filterParams := systemActionNestedParams(params, "filters")
	if filterParams == nil {
		filterParams = params
	}
	source := systemActionStringParam(filterParams, "source")
	if source == "<nil>" {
		source = ""
	}
	filter := store.ItemListFilter{
		Source: source,
	}
	workspaceValue := strings.TrimSpace(fmt.Sprint(filterParams["workspace_id"]))
	if workspaceValue != "" && workspaceValue != "<nil>" {
		if strings.EqualFold(workspaceValue, "null") {
			filter.WorkspaceUnassigned = true
		} else {
			workspaceID := systemActionItemID(map[string]interface{}{"item_id": filterParams["workspace_id"]})
			if workspaceID <= 0 {
				return store.ItemListFilter{}, errors.New("workspace_id must be positive or null")
			}
			filter.WorkspaceID = &workspaceID
		}
	}
	projectID := systemActionStringParam(filterParams, "project_id")
	if projectID != "" && projectID != "<nil>" {
		filter.ProjectID = &projectID
	}
	return filter, nil
}

func systemActionAllSpheresParam(params map[string]interface{}) bool {
	if params == nil {
		return false
	}
	if systemActionTruthyParam(params, "all_spheres") {
		return true
	}
	nested := systemActionNestedParams(params, "filters")
	return systemActionTruthyParam(nested, "all_spheres")
}

func filteredItemViewMessage(view string, filter store.ItemListFilter, count int, allSpheres bool) string {
	listName := "inbox"
	if view == store.ItemStateWaiting || view == store.ItemStateSomeday || view == store.ItemStateDone {
		listName = view
	}
	filterText := ""
	if allSpheres {
		filterText = " across all spheres"
	}
	switch {
	case filter.Source != "":
		filterText += fmt.Sprintf(" filtered to %s", filter.Source)
	case filter.WorkspaceUnassigned:
		filterText += " filtered to unassigned items"
	case filter.WorkspaceID != nil:
		filterText += " filtered to one workspace"
	case filter.ProjectID != nil:
		filterText += " filtered to one project"
	}
	if count == 0 {
		return fmt.Sprintf("Opened %s%s. There are no matching items right now.", listName, filterText)
	}
	return fmt.Sprintf("Opened %s%s with %d item(s).", listName, filterText, count)
}

func itemFilterPayload(filter store.ItemListFilter, allSpheres bool) map[string]interface{} {
	payload := map[string]interface{}{}
	if filter.Source != "" {
		payload["source"] = filter.Source
	}
	if filter.WorkspaceID != nil {
		payload["workspace_id"] = *filter.WorkspaceID
	}
	if filter.WorkspaceUnassigned {
		payload["workspace_id"] = "null"
	}
	if filter.ProjectID != nil {
		payload["project_id"] = *filter.ProjectID
	}
	if allSpheres {
		payload["all_spheres"] = true
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func (a *App) executeFilteredItemViewAction(action *SystemAction) (string, map[string]interface{}, error) {
	view := strings.ToLower(strings.TrimSpace(systemActionStringParam(action.Params, "view")))
	switch view {
	case "", store.ItemStateInbox:
		view = store.ItemStateInbox
	case store.ItemStateWaiting, store.ItemStateSomeday, store.ItemStateDone:
	default:
		return "", nil, errors.New("view must be inbox, waiting, someday, or done")
	}

	filter, err := itemFilterFromActionParams(action.Params)
	if err != nil {
		return "", nil, err
	}
	allSpheres := systemActionAllSpheresParam(action.Params)
	if !allSpheres {
		activeSphere, err := a.store.ActiveSphere()
		if err != nil {
			return "", nil, err
		}
		filter.Sphere = activeSphere
	}

	var count int
	switch view {
	case store.ItemStateInbox:
		items, err := a.store.ListInboxItemsFiltered(time.Now().UTC(), filter)
		if err != nil {
			return "", nil, err
		}
		count = len(items)
	case store.ItemStateWaiting:
		items, err := a.store.ListWaitingItemsFiltered(filter)
		if err != nil {
			return "", nil, err
		}
		count = len(items)
	case store.ItemStateSomeday:
		items, err := a.store.ListSomedayItemsFiltered(filter)
		if err != nil {
			return "", nil, err
		}
		count = len(items)
	case store.ItemStateDone:
		items, err := a.store.ListDoneItemsFiltered(50, filter)
		if err != nil {
			return "", nil, err
		}
		count = len(items)
	}

	payload := map[string]interface{}{
		"type":  "show_item_sidebar_view",
		"view":  view,
		"count": count,
	}
	if systemActionTruthyParam(action.Params, "clear_filters") {
		payload["clear_filters"] = true
		if allSpheres {
			payload["filters"] = map[string]interface{}{"all_spheres": true}
		}
	} else if filters := itemFilterPayload(filter, allSpheres); filters != nil {
		payload["filters"] = filters
	} else if allSpheres {
		payload["filters"] = map[string]interface{}{"all_spheres": true}
	}
	return filteredItemViewMessage(view, filter, count, allSpheres), payload, nil
}
