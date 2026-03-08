package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

type itemGitHubPRReviewSyncResponse struct {
	OK          bool   `json:"ok"`
	WorkspaceID int64  `json:"workspace_id"`
	Repo        string `json:"repo"`
	Synced      int    `json:"synced"`
	Requested   int    `json:"requested"`
	Closed      int    `json:"closed"`
}

type ghPRReviewListItem struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
}

func githubPRReviewSourceRef(ownerRepo string, number int) string {
	return fmt.Sprintf("%s#PR-%d", strings.TrimSpace(ownerRepo), number)
}

func githubPRReviewDiffURL(raw string) string {
	clean := strings.TrimRight(strings.TrimSpace(raw), "/")
	if clean == "" {
		return ""
	}
	return clean + ".diff"
}

func githubPRArtifactMeta(ownerRepo string, pr ghPRReviewListItem) (*string, error) {
	payload := map[string]any{
		"owner_repo":    ownerRepo,
		"number":        pr.Number,
		"state":         "open",
		"url":           strings.TrimSpace(pr.URL),
		"diff_url":      githubPRReviewDiffURL(pr.URL),
		"head_ref_name": strings.TrimSpace(pr.HeadRefName),
		"base_ref_name": strings.TrimSpace(pr.BaseRefName),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	return &text, nil
}

func (a *App) listGitHubPRReviewRequests(cwd string) ([]ghPRReviewListItem, error) {
	runner := a.ghCommandRunner
	if runner == nil {
		runner = runGitHubCLI
	}
	ctx, cancel := context.WithTimeout(context.Background(), githubPRCommandTimeout)
	defer cancel()

	raw, err := runner(
		ctx,
		cwd,
		"pr", "list",
		"--search", "review-requested:@me",
		"--state", "open",
		"--limit", "500",
		"--json", "number,title,url,headRefName,baseRefName",
	)
	if err != nil {
		return nil, err
	}
	var prs []ghPRReviewListItem
	if err := json.Unmarshal([]byte(raw), &prs); err != nil {
		return nil, fmt.Errorf("invalid github pr response: %w", err)
	}
	return prs, nil
}

func (a *App) syncGitHubPRReviewArtifact(item store.Item, ownerRepo string, pr ghPRReviewListItem) error {
	kind := store.ArtifactKindGitHubPR
	refURL := optionalTrimmedString(pr.URL)
	title := optionalTrimmedString(fmt.Sprintf("PR #%d", pr.Number))
	metaJSON, err := githubPRArtifactMeta(ownerRepo, pr)
	if err != nil {
		return err
	}

	createArtifact := func() (store.Artifact, error) {
		return a.store.CreateArtifact(kind, nil, refURL, title, metaJSON)
	}

	if item.ArtifactID == nil {
		artifact, err := createArtifact()
		if err != nil {
			return err
		}
		return a.store.UpdateItemArtifact(item.ID, &artifact.ID)
	}

	err = a.store.UpdateArtifact(*item.ArtifactID, store.ArtifactUpdate{
		Kind:     &kind,
		RefURL:   refURL,
		Title:    title,
		MetaJSON: metaJSON,
	})
	if errors.Is(err, sql.ErrNoRows) {
		artifact, createErr := createArtifact()
		if createErr != nil {
			return createErr
		}
		return a.store.UpdateItemArtifact(item.ID, &artifact.ID)
	}
	return err
}

func isGitHubPRReviewItem(item store.Item, ownerRepo string, workspaceID int64) bool {
	if item.WorkspaceID == nil || *item.WorkspaceID != workspaceID {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(stringFromPointer(item.Source)), "github") {
		return false
	}
	sourceRef := strings.TrimSpace(stringFromPointer(item.SourceRef))
	return strings.HasPrefix(sourceRef, strings.TrimSpace(ownerRepo)+"#PR-")
}

func (a *App) syncGitHubPRReviews(workspaceID int64) (itemGitHubPRReviewSyncResponse, error) {
	workspace, err := a.store.GetWorkspace(workspaceID)
	if err != nil {
		return itemGitHubPRReviewSyncResponse{}, err
	}
	repo, err := a.store.GitHubRepoForWorkspace(workspaceID)
	if err != nil {
		return itemGitHubPRReviewSyncResponse{}, err
	}
	if strings.TrimSpace(repo) == "" {
		return itemGitHubPRReviewSyncResponse{}, errors.New("workspace has no GitHub origin remote")
	}

	prs, err := a.listGitHubPRReviewRequests(workspace.DirPath)
	if err != nil {
		return itemGitHubPRReviewSyncResponse{}, err
	}

	result := itemGitHubPRReviewSyncResponse{
		OK:          true,
		WorkspaceID: workspace.ID,
		Repo:        repo,
		Synced:      len(prs),
	}
	activeRefs := make(map[string]struct{}, len(prs))
	for _, pr := range prs {
		if pr.Number <= 0 {
			return itemGitHubPRReviewSyncResponse{}, errors.New("github pr number is required")
		}
		if strings.TrimSpace(pr.Title) == "" {
			return itemGitHubPRReviewSyncResponse{}, fmt.Errorf("github pr #%d title is required", pr.Number)
		}
		sourceRef := githubPRReviewSourceRef(repo, pr.Number)
		activeRefs[sourceRef] = struct{}{}

		item, err := a.store.UpsertItemFromSource("github", sourceRef, pr.Title, &workspace.ID)
		if err != nil {
			return itemGitHubPRReviewSyncResponse{}, err
		}
		if err := a.syncGitHubPRReviewArtifact(item, repo, pr); err != nil {
			return itemGitHubPRReviewSyncResponse{}, err
		}
		if item.State == store.ItemStateDone {
			if err := a.store.SyncItemStateBySource("github", sourceRef, store.ItemStateInbox); err != nil {
				return itemGitHubPRReviewSyncResponse{}, err
			}
		}
		result.Requested++
	}

	items, err := a.store.ListItems()
	if err != nil {
		return itemGitHubPRReviewSyncResponse{}, err
	}
	for _, item := range items {
		if !isGitHubPRReviewItem(item, repo, workspace.ID) {
			continue
		}
		sourceRef := strings.TrimSpace(stringFromPointer(item.SourceRef))
		if _, ok := activeRefs[sourceRef]; ok || item.State == store.ItemStateDone {
			continue
		}
		if err := a.store.SyncItemStateBySource("github", sourceRef, store.ItemStateDone); err != nil {
			return itemGitHubPRReviewSyncResponse{}, err
		}
		result.Closed++
	}

	return result, nil
}

func (a *App) handleGitHubPRReviewSync(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req itemGitHubSyncRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID <= 0 {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}

	result, err := a.syncGitHubPRReviews(req.WorkspaceID)
	if err != nil {
		switch {
		case strings.Contains(strings.ToLower(err.Error()), "no github origin remote"):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, sql.ErrNoRows):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	writeJSON(w, result)
}
