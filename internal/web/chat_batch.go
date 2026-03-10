package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

const (
	batchModeWatch = "watch"
	batchModeRun   = "batch"
)

var (
	batchWorkAllPattern    = regexp.MustCompile(`(?i)^work through (?:all )?open issues$`)
	batchWorkLabelPattern  = regexp.MustCompile(`(?i)^work through ([pP][0-3]) issues$`)
	batchWorkRangePattern  = regexp.MustCompile(`(?i)^work through (?:issues )?(\d+)\s*-\s*(\d+)$`)
	batchConfigPattern     = regexp.MustCompile(`(?i)^use ([a-z0-9._-]+) for work(?:,| and)\s*([a-z0-9._-]+) for review$`)
	batchLimitPattern      = regexp.MustCompile(`(?i)^stop after (\d+)(?: items?)?$`)
	batchIssueNumberRegexp = regexp.MustCompile(`#(\d+)$`)
)

type batchWorkConfig struct {
	Mode         string `json:"mode,omitempty"`
	Worker       string `json:"worker,omitempty"`
	Reviewer     string `json:"reviewer,omitempty"`
	LabelFilter  string `json:"label_filter,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	IssueNumbers []int  `json:"issue_numbers,omitempty"`
}

type batchItemCounts struct {
	Pending   int
	Running   int
	Completed int
	Failed    int
}

func parseInlineBatchIntent(text string) *SystemAction {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	switch normalizeItemCommandText(trimmed) {
	case "show me progress", "show progress", "batch status", "show batch status":
		return &SystemAction{Action: "batch_status", Params: map[string]interface{}{}}
	}
	if match := batchConfigPattern.FindStringSubmatch(trimmed); len(match) == 3 {
		return &SystemAction{
			Action: "batch_configure",
			Params: map[string]interface{}{
				"worker":   strings.TrimSpace(match[1]),
				"reviewer": strings.TrimSpace(match[2]),
			},
		}
	}
	if match := batchLimitPattern.FindStringSubmatch(trimmed); len(match) == 2 {
		limit, err := strconv.Atoi(strings.TrimSpace(match[1]))
		if err == nil && limit > 0 {
			return &SystemAction{
				Action: "batch_limit",
				Params: map[string]interface{}{"limit": limit},
			}
		}
	}
	if batchWorkAllPattern.MatchString(trimmed) {
		return &SystemAction{Action: "batch_work", Params: map[string]interface{}{}}
	}
	if match := batchWorkLabelPattern.FindStringSubmatch(trimmed); len(match) == 2 {
		return &SystemAction{
			Action: "batch_work",
			Params: map[string]interface{}{"label_filter": normalizeBatchLabelFilter(match[1])},
		}
	}
	if match := batchWorkRangePattern.FindStringSubmatch(trimmed); len(match) == 3 {
		start, startErr := strconv.Atoi(strings.TrimSpace(match[1]))
		end, endErr := strconv.Atoi(strings.TrimSpace(match[2]))
		if startErr == nil && endErr == nil && start > 0 && end > 0 {
			return &SystemAction{
				Action: "batch_work",
				Params: map[string]interface{}{"issue_numbers": batchIssueRange(start, end)},
			}
		}
	}
	return nil
}

func normalizeBatchLabelFilter(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeBatchWorkConfig(cfg batchWorkConfig) batchWorkConfig {
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode != batchModeRun {
		cfg.Mode = ""
	}
	cfg.Worker = strings.TrimSpace(cfg.Worker)
	cfg.Reviewer = strings.TrimSpace(cfg.Reviewer)
	cfg.LabelFilter = normalizeBatchLabelFilter(cfg.LabelFilter)
	if cfg.Limit < 0 {
		cfg.Limit = 0
	}
	if len(cfg.IssueNumbers) == 0 {
		cfg.IssueNumbers = nil
		return cfg
	}
	seen := make(map[int]struct{}, len(cfg.IssueNumbers))
	out := make([]int, 0, len(cfg.IssueNumbers))
	for _, value := range cfg.IssueNumbers {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Ints(out)
	cfg.IssueNumbers = out
	return cfg
}

func batchConfigMode(cfg batchWorkConfig) string {
	if strings.EqualFold(strings.TrimSpace(cfg.Mode), batchModeRun) {
		return batchModeRun
	}
	return batchModeWatch
}

func decodeBatchWorkConfig(raw string) (batchWorkConfig, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" || clean == "{}" {
		return batchWorkConfig{}, nil
	}
	var cfg batchWorkConfig
	if err := json.Unmarshal([]byte(clean), &cfg); err != nil {
		return batchWorkConfig{}, err
	}
	return normalizeBatchWorkConfig(cfg), nil
}

func encodeBatchWorkConfig(cfg batchWorkConfig) (string, error) {
	normalized := normalizeBatchWorkConfig(cfg)
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func batchConfigPayload(cfg batchWorkConfig) map[string]interface{} {
	payload := map[string]interface{}{
		"mode": batchConfigMode(cfg),
	}
	if cfg.Worker != "" {
		payload["worker"] = cfg.Worker
	}
	if cfg.Reviewer != "" {
		payload["reviewer"] = cfg.Reviewer
	}
	if cfg.LabelFilter != "" {
		payload["label_filter"] = cfg.LabelFilter
	}
	if cfg.Limit > 0 {
		payload["limit"] = cfg.Limit
	}
	if len(cfg.IssueNumbers) > 0 {
		numbers := make([]int, len(cfg.IssueNumbers))
		copy(numbers, cfg.IssueNumbers)
		payload["issue_numbers"] = numbers
	}
	return payload
}

func batchIssueRange(start, end int) []int {
	if start > end {
		start, end = end, start
	}
	out := make([]int, 0, end-start+1)
	for value := start; value <= end; value++ {
		out = append(out, value)
	}
	return out
}

func systemActionIntParam(params map[string]interface{}, key string) int {
	switch value := params[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func systemActionIntListParam(params map[string]interface{}, key string) []int {
	value, ok := params[key]
	if !ok || value == nil {
		return nil
	}
	out := []int{}
	appendValue := func(raw int) {
		if raw > 0 {
			out = append(out, raw)
		}
	}
	switch typed := value.(type) {
	case []int:
		for _, item := range typed {
			appendValue(item)
		}
	case []interface{}:
		for _, item := range typed {
			switch parsed := item.(type) {
			case int:
				appendValue(parsed)
			case int64:
				appendValue(int(parsed))
			case float64:
				appendValue(int(parsed))
			case string:
				number, err := strconv.Atoi(strings.TrimSpace(parsed))
				if err == nil {
					appendValue(number)
				}
			}
		}
	case string:
		for _, part := range strings.Split(typed, ",") {
			number, err := strconv.Atoi(strings.TrimSpace(part))
			if err == nil {
				appendValue(number)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return normalizeBatchWorkConfig(batchWorkConfig{IssueNumbers: out}).IssueNumbers
}

func batchStatusCounts(items []store.BatchRunItem) batchItemCounts {
	var counts batchItemCounts
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "running":
			counts.Running++
		case "done", "completed":
			counts.Completed++
		case "failed", "canceled":
			counts.Failed++
		default:
			counts.Pending++
		}
	}
	return counts
}

func summarizeBatchRun(workspace store.Workspace, run *store.BatchRun, items []store.BatchRunItem, active bool) string {
	if run == nil {
		return fmt.Sprintf("No batch has run for workspace %s.", workspace.Name)
	}
	counts := batchStatusCounts(items)
	parts := []string{
		fmt.Sprintf("%d completed", counts.Completed),
		fmt.Sprintf("%d failed", counts.Failed),
	}
	if counts.Running > 0 {
		parts = append(parts, fmt.Sprintf("%d running", counts.Running))
	}
	if counts.Pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", counts.Pending))
	}
	status := strings.TrimSpace(run.Status)
	if status == "" {
		status = "running"
	}
	if active {
		return fmt.Sprintf("Batch for workspace %s is %s: %s.", workspace.Name, status, strings.Join(parts, ", "))
	}
	return fmt.Sprintf("Latest batch for workspace %s finished %s: %s.", workspace.Name, status, strings.Join(parts, ", "))
}

func (a *App) loadWorkspaceBatchConfig(workspaceID int64) (batchWorkConfig, *store.WorkspaceWatch, error) {
	watch, err := a.store.GetWorkspaceWatch(workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return batchWorkConfig{}, nil, nil
	}
	if err != nil {
		return batchWorkConfig{}, nil, err
	}
	cfg, err := decodeBatchWorkConfig(watch.ConfigJSON)
	if err != nil {
		return batchWorkConfig{}, nil, err
	}
	return cfg, &watch, nil
}

func (a *App) saveWorkspaceBatchConfig(workspaceID int64, cfg batchWorkConfig, enabled bool, pollInterval int, currentBatchID *int64) (store.WorkspaceWatch, error) {
	configJSON, err := encodeBatchWorkConfig(cfg)
	if err != nil {
		return store.WorkspaceWatch{}, err
	}
	if pollInterval <= 0 {
		pollInterval = 300
	}
	return a.store.UpsertWorkspaceWatch(workspaceID, configJSON, pollInterval, enabled, currentBatchID)
}

func (a *App) listBatchCandidateItems(workspaceID int64, cfg batchWorkConfig) ([]store.ItemSummary, error) {
	filter := store.ItemListFilter{WorkspaceID: &workspaceID}
	items, err := a.store.ListInboxItemsFiltered(time.Now().UTC(), filter)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt < items[j].CreatedAt
	})
	if cfg.LabelFilter == "" && len(cfg.IssueNumbers) == 0 {
		return items, nil
	}
	out := make([]store.ItemSummary, 0, len(items))
	for _, item := range items {
		if a.itemMatchesBatchConfig(item, cfg) {
			out = append(out, item)
		}
	}
	return out, nil
}

func batchItemIssueNumber(item store.ItemSummary) int {
	match := batchIssueNumberRegexp.FindStringSubmatch(strings.TrimSpace(stringFromPointer(item.SourceRef)))
	if len(match) != 2 {
		return 0
	}
	value, err := strconv.Atoi(match[1])
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func batchArtifactLabels(metaJSON *string) []string {
	if metaJSON == nil || strings.TrimSpace(*metaJSON) == "" {
		return nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(*metaJSON)), &payload); err != nil {
		return nil
	}
	raw, ok := payload["labels"]
	if !ok {
		return nil
	}
	values, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		label := normalizeBatchLabelFilter(fmt.Sprint(value))
		if label != "" {
			out = append(out, label)
		}
	}
	return out
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (a *App) itemMatchesBatchConfig(item store.ItemSummary, cfg batchWorkConfig) bool {
	cfg = normalizeBatchWorkConfig(cfg)
	if len(cfg.IssueNumbers) > 0 {
		if number := batchItemIssueNumber(item); number <= 0 || !containsInt(cfg.IssueNumbers, number) {
			return false
		}
	}
	if cfg.LabelFilter == "" {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(item.Title)), cfg.LabelFilter) {
		return true
	}
	if item.ArtifactID == nil || *item.ArtifactID <= 0 {
		return false
	}
	artifact, err := a.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		return false
	}
	for _, label := range batchArtifactLabels(artifact.MetaJSON) {
		if label == cfg.LabelFilter {
			return true
		}
	}
	return false
}

func (a *App) latestBatchRunForWorkspace(workspaceID int64) (*store.BatchRun, error) {
	runs, err := a.store.ListBatchRuns(&workspaceID)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return &runs[0], nil
}

func (a *App) executeBatchStatusAction(workspace store.Workspace) (string, map[string]interface{}, error) {
	cfg, watch, err := a.loadWorkspaceBatchConfig(workspace.ID)
	if err != nil {
		return "", nil, err
	}
	var status workspaceWatchStatus
	if snapshot, ok := a.workspaceWatches.snapshot(workspace.ID); ok {
		status = snapshot
	}
	var run *store.BatchRun
	switch {
	case watch != nil && watch.CurrentBatchID != nil && *watch.CurrentBatchID > 0:
		current, err := a.store.GetBatchRun(*watch.CurrentBatchID)
		if err == nil {
			run = &current
		}
	case true:
		run, err = a.latestBatchRunForWorkspace(workspace.ID)
		if err != nil {
			return "", nil, err
		}
	}
	items := []store.BatchRunItem{}
	if run != nil {
		items, err = a.store.ListBatchRunItems(run.ID)
		if err != nil {
			return "", nil, err
		}
	}
	payload := map[string]interface{}{
		"type":         "batch_status",
		"workspace_id": workspace.ID,
		"config":       batchConfigPayload(cfg),
		"status":       status,
	}
	if watch != nil {
		payload["watch"] = watch
	}
	if run != nil {
		payload["batch"] = run
		payload["items"] = items
	}
	return summarizeBatchRun(workspace, run, items, status.Active), payload, nil
}

func (a *App) executeBatchAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	workspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionWorkspaceRef(action.Params))
	if err != nil {
		return "", nil, err
	}
	cfg, watch, err := a.loadWorkspaceBatchConfig(workspace.ID)
	if err != nil {
		return "", nil, err
	}
	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "batch_configure":
		worker := strings.TrimSpace(systemActionStringParam(action.Params, "worker"))
		reviewer := strings.TrimSpace(systemActionStringParam(action.Params, "reviewer"))
		if worker == "" || reviewer == "" {
			return "", nil, errors.New("batch worker and reviewer are required")
		}
		cfg.Worker = worker
		cfg.Reviewer = reviewer
		pollInterval := 300
		enabled := false
		var currentBatchID *int64
		if watch != nil {
			pollInterval = watch.PollIntervalSeconds
			enabled = watch.Enabled
			currentBatchID = watch.CurrentBatchID
		}
		updatedWatch, err := a.saveWorkspaceBatchConfig(workspace.ID, cfg, enabled, pollInterval, currentBatchID)
		if err != nil {
			return "", nil, err
		}
		if enabled {
			a.reconcileWorkspaceWatches()
		}
		return fmt.Sprintf("Batch config for workspace %s set to worker %s and reviewer %s.", workspace.Name, worker, reviewer), map[string]interface{}{
			"type":         "batch_status",
			"workspace_id": workspace.ID,
			"watch":        updatedWatch,
			"config":       batchConfigPayload(cfg),
		}, nil
	case "batch_limit":
		limit := systemActionIntParam(action.Params, "limit")
		if limit <= 0 {
			return "", nil, errors.New("batch limit must be positive")
		}
		cfg.Mode = batchModeRun
		cfg.Limit = limit
		pollInterval := 300
		enabled := false
		var currentBatchID *int64
		if watch != nil {
			pollInterval = watch.PollIntervalSeconds
			enabled = watch.Enabled
			currentBatchID = watch.CurrentBatchID
		}
		updatedWatch, err := a.saveWorkspaceBatchConfig(workspace.ID, cfg, enabled, pollInterval, currentBatchID)
		if err != nil {
			return "", nil, err
		}
		if enabled {
			a.reconcileWorkspaceWatches()
		}
		return fmt.Sprintf("Batch limit for workspace %s set to %d item(s).", workspace.Name, limit), map[string]interface{}{
			"type":         "batch_status",
			"workspace_id": workspace.ID,
			"watch":        updatedWatch,
			"config":       batchConfigPayload(cfg),
		}, nil
	case "batch_status":
		return a.executeBatchStatusAction(workspace)
	case "batch_work":
		if snapshot, ok := a.workspaceWatches.snapshot(workspace.ID); ok && snapshot.Active {
			message, payload, err := a.executeBatchStatusAction(workspace)
			if err != nil {
				return "", nil, err
			}
			return "Batch already running for workspace " + workspace.Name + ".\n\n" + message, payload, nil
		}
		cfg.Mode = batchModeRun
		if labelFilter := normalizeBatchLabelFilter(systemActionStringParam(action.Params, "label_filter")); labelFilter != "" {
			cfg.LabelFilter = labelFilter
			cfg.IssueNumbers = nil
		} else if issueNumbers := systemActionIntListParam(action.Params, "issue_numbers"); len(issueNumbers) > 0 {
			cfg.IssueNumbers = issueNumbers
			cfg.LabelFilter = ""
		} else {
			cfg.LabelFilter = ""
			cfg.IssueNumbers = nil
		}
		candidates, err := a.listBatchCandidateItems(workspace.ID, cfg)
		if err != nil {
			return "", nil, err
		}
		if len(candidates) == 0 {
			return fmt.Sprintf("No matching open items in workspace %s.", workspace.Name), map[string]interface{}{
				"type":         "batch_status",
				"workspace_id": workspace.ID,
				"config":       batchConfigPayload(cfg),
				"item_count":   0,
			}, nil
		}
		configJSON, err := encodeBatchWorkConfig(cfg)
		if err != nil {
			return "", nil, err
		}
		run, err := a.store.CreateBatchRun(workspace.ID, configJSON, "running")
		if err != nil {
			return "", nil, err
		}
		pollInterval := 1
		if watch != nil && watch.PollIntervalSeconds > 0 {
			pollInterval = watch.PollIntervalSeconds
		}
		updatedWatch, err := a.store.UpsertWorkspaceWatch(workspace.ID, configJSON, pollInterval, true, &run.ID)
		if err != nil {
			return "", nil, err
		}
		a.reconcileWorkspaceWatches()
		_, status, snapshotErr := a.workspaceWatchSnapshot(workspace.ID)
		if snapshotErr != nil {
			status = workspaceWatchStatus{WorkspaceID: workspace.ID}
		}
		return fmt.Sprintf("Started batch for workspace %s with %d open item(s).", workspace.Name, len(candidates)), map[string]interface{}{
			"type":         "batch_status",
			"workspace_id": workspace.ID,
			"watch":        updatedWatch,
			"status":       status,
			"batch":        run,
			"config":       batchConfigPayload(cfg),
			"item_count":   len(candidates),
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported batch action: %s", action.Action)
	}
}
