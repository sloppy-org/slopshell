package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

const (
	materializedRootDir = ".tabura/materialized"
	archiveManifestPath = ".tabura/manifest.json"
)

type materializeConfig struct {
	dataDir string
}

type materializedArtifact struct {
	Artifact      store.Artifact
	AbsolutePath  string
	RelativePath  string
	Format        string
	StoreUpdated  bool
	OriginalPath  string
	Materialized  bool
	CopiedFromRef bool
}

type archiveResult struct {
	Workspace      store.Workspace
	ManifestPath   string
	Artifacts      []materializedArtifact
	Items          []store.Item
	Materialized   int
	ExistingInside int
	CopiedExisting int
}

type archiveManifest struct {
	Workspace archiveWorkspaceManifest `json:"workspace"`
	Artifacts []archiveArtifactEntry   `json:"artifacts"`
	Items     []archiveItemEntry       `json:"items"`
}

type archiveWorkspaceManifest struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	DirPath   string  `json:"dir_path"`
	Sphere    string  `json:"sphere"`
	ProjectID *string `json:"project_id,omitempty"`
}

type archiveArtifactEntry struct {
	ID               int64                 `json:"id"`
	Kind             string                `json:"kind"`
	Title            *string               `json:"title,omitempty"`
	OriginalRefPath  *string               `json:"original_ref_path,omitempty"`
	RefURL           *string               `json:"ref_url,omitempty"`
	MaterializedPath string                `json:"materialized_path"`
	Labels           []string              `json:"labels,omitempty"`
	Bindings         []archiveBindingEntry `json:"bindings,omitempty"`
}

type archiveBindingEntry struct {
	Provider        string  `json:"provider"`
	ObjectType      string  `json:"object_type"`
	RemoteID        string  `json:"remote_id"`
	ContainerRef    *string `json:"container_ref,omitempty"`
	RemoteUpdatedAt *string `json:"remote_updated_at,omitempty"`
}

type archiveItemEntry struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	State       string   `json:"state"`
	WorkspaceID *int64   `json:"workspace_id,omitempty"`
	ProjectID   *string  `json:"project_id,omitempty"`
	ArtifactID  *int64   `json:"artifact_id,omitempty"`
	Source      *string  `json:"source,omitempty"`
	SourceRef   *string  `json:"source_ref,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

func cmdMaterialize(args []string) int {
	cfg, artifactID, workspacePath, status := parseMaterializeArgs(args)
	if status != 0 {
		return status
	}
	st, err := openCLIStore(cfg.dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()

	result, err := materializeArtifact(st, artifactID, workspacePath, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("materialized artifact %d to %s\n", result.Artifact.ID, result.AbsolutePath)
	return 0
}

func cmdArchive(args []string) int {
	cfg, workspacePath, status := parseArchiveArgs(args)
	if status != 0 {
		return status
	}
	st, err := openCLIStore(cfg.dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()

	result, err := archiveWorkspace(st, workspacePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf(
		"archived workspace %s: manifest=%s artifacts=%d materialized=%d copied=%d existing=%d items=%d\n",
		result.Workspace.Name,
		result.ManifestPath,
		len(result.Artifacts),
		result.Materialized,
		result.CopiedExisting,
		result.ExistingInside,
		len(result.Items),
	)
	return 0
}

func parseMaterializeArgs(args []string) (materializeConfig, int64, string, int) {
	fs := flag.NewFlagSet("materialize", flag.ContinueOnError)
	cfg := materializeConfig{dataDir: filepath.Join(os.Getenv("HOME"), ".tabura-web")}
	fs.StringVar(&cfg.dataDir, "data-dir", cfg.dataDir, "data dir")
	if err := fs.Parse(args); err != nil {
		return materializeConfig{}, 0, "", 2
	}
	rest := fs.Args()
	if len(rest) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tabura materialize [--data-dir DIR] <artifact-id> <workspace-path>")
		return materializeConfig{}, 0, "", 2
	}
	artifactID, err := strconv.ParseInt(strings.TrimSpace(rest[0]), 10, 64)
	if err != nil || artifactID <= 0 {
		fmt.Fprintln(os.Stderr, "artifact id must be a positive integer")
		return materializeConfig{}, 0, "", 2
	}
	return cfg, artifactID, rest[1], 0
}

func parseArchiveArgs(args []string) (materializeConfig, string, int) {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	cfg := materializeConfig{dataDir: filepath.Join(os.Getenv("HOME"), ".tabura-web")}
	fs.StringVar(&cfg.dataDir, "data-dir", cfg.dataDir, "data dir")
	if err := fs.Parse(args); err != nil {
		return materializeConfig{}, "", 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: tabura archive [--data-dir DIR] <workspace-path>")
		return materializeConfig{}, "", 2
	}
	return cfg, rest[0], 0
}

func openCLIStore(dataDir string) (*store.Store, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("data dir is required")
	}
	return store.New(filepath.Join(strings.TrimSpace(dataDir), "tabura.db"))
}

func archiveWorkspace(st *store.Store, workspacePath string) (archiveResult, error) {
	workspace, err := st.GetWorkspaceByPath(workspacePath)
	if err != nil {
		return archiveResult{}, err
	}
	artifacts, err := st.ListArtifactsForWorkspace(workspace.ID)
	if err != nil {
		return archiveResult{}, err
	}
	items, err := st.ListItemsFiltered(store.ItemListFilter{WorkspaceID: &workspace.ID})
	if err != nil {
		return archiveResult{}, err
	}

	results := make([]materializedArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		result, err := materializeArtifactForArchive(st, artifact, workspace.DirPath)
		if err != nil {
			return archiveResult{}, err
		}
		results = append(results, result)
	}

	manifest := archiveManifest{
		Workspace: archiveWorkspaceManifest{
			ID:        workspace.ID,
			Name:      workspace.Name,
			DirPath:   workspace.DirPath,
			Sphere:    workspace.Sphere,
			ProjectID: workspace.ProjectID,
		},
		Artifacts: make([]archiveArtifactEntry, 0, len(results)),
		Items:     make([]archiveItemEntry, 0, len(items)),
	}

	counts := archiveResult{
		Workspace: workspace,
		Artifacts: results,
		Items:     items,
	}
	for _, result := range results {
		if result.Materialized {
			counts.Materialized++
		}
		if result.CopiedFromRef {
			counts.CopiedExisting++
		}
		if !result.Materialized && !result.CopiedFromRef {
			counts.ExistingInside++
		}
		entry, err := buildArchiveArtifactEntry(st, workspace, result)
		if err != nil {
			return archiveResult{}, err
		}
		manifest.Artifacts = append(manifest.Artifacts, entry)
	}
	for _, item := range items {
		manifest.Items = append(manifest.Items, archiveItemEntry{
			ID:          item.ID,
			Title:       item.Title,
			State:       item.State,
			WorkspaceID: item.WorkspaceID,
			ProjectID:   item.ProjectID,
			ArtifactID:  item.ArtifactID,
			Source:      item.Source,
			SourceRef:   item.SourceRef,
			Labels:      itemArchiveLabels(item),
		})
	}
	sort.Slice(manifest.Artifacts, func(i, j int) bool { return manifest.Artifacts[i].ID < manifest.Artifacts[j].ID })
	sort.Slice(manifest.Items, func(i, j int) bool { return manifest.Items[i].ID < manifest.Items[j].ID })

	manifestAbs := filepath.Join(workspace.DirPath, archiveManifestPath)
	if err := os.MkdirAll(filepath.Dir(manifestAbs), 0o755); err != nil {
		return archiveResult{}, err
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return archiveResult{}, err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(manifestAbs, raw, 0o644); err != nil {
		return archiveResult{}, err
	}
	counts.ManifestPath = manifestAbs
	return counts, nil
}

func buildArchiveArtifactEntry(st *store.Store, workspace store.Workspace, result materializedArtifact) (archiveArtifactEntry, error) {
	bindings, err := st.GetBindingsByArtifact(result.Artifact.ID)
	if err != nil {
		return archiveArtifactEntry{}, err
	}
	bindingEntries := make([]archiveBindingEntry, 0, len(bindings))
	for _, binding := range bindings {
		bindingEntries = append(bindingEntries, archiveBindingEntry{
			Provider:        binding.Provider,
			ObjectType:      binding.ObjectType,
			RemoteID:        binding.RemoteID,
			ContainerRef:    binding.ContainerRef,
			RemoteUpdatedAt: binding.RemoteUpdatedAt,
		})
	}
	return archiveArtifactEntry{
		ID:               result.Artifact.ID,
		Kind:             string(result.Artifact.Kind),
		Title:            result.Artifact.Title,
		OriginalRefPath:  nullableString(result.OriginalPath),
		RefURL:           result.Artifact.RefURL,
		MaterializedPath: result.RelativePath,
		Labels:           artifactArchiveLabels(st, workspace, result.Artifact),
		Bindings:         bindingEntries,
	}, nil
}

func itemArchiveLabels(item store.Item) []string {
	labels := make([]string, 0, 1)
	if item.ProjectID != nil && strings.TrimSpace(*item.ProjectID) != "" {
		labels = append(labels, strings.TrimSpace(*item.ProjectID))
	}
	return labels
}

func artifactArchiveLabels(st *store.Store, workspace store.Workspace, artifact store.Artifact) []string {
	seen := map[string]struct{}{}
	labels := []string{}
	if workspace.ProjectID != nil {
		if clean := strings.TrimSpace(*workspace.ProjectID); clean != "" {
			labels = appendUniqueLabel(labels, seen, clean)
		}
	}
	for _, label := range metaLabels(artifact.MetaJSON) {
		labels = appendUniqueLabel(labels, seen, label)
	}
	items, err := st.ListArtifactItems(artifact.ID)
	if err == nil {
		for _, item := range items {
			if item.ProjectID != nil {
				labels = appendUniqueLabel(labels, seen, strings.TrimSpace(*item.ProjectID))
			}
		}
	}
	return labels
}

func appendUniqueLabel(labels []string, seen map[string]struct{}, raw string) []string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return labels
	}
	if _, ok := seen[clean]; ok {
		return labels
	}
	seen[clean] = struct{}{}
	return append(labels, clean)
}

func materializeArtifact(st *store.Store, artifactID int64, workspacePath string, updateStore bool) (materializedArtifact, error) {
	artifact, err := st.GetArtifact(artifactID)
	if err != nil {
		return materializedArtifact{}, err
	}
	return materializeArtifactWithOptions(st, artifact, workspacePath, updateStore)
}

func materializeArtifactForArchive(st *store.Store, artifact store.Artifact, workspacePath string) (materializedArtifact, error) {
	return materializeArtifactWithOptions(st, artifact, workspacePath, false)
}

func materializeArtifactWithOptions(st *store.Store, artifact store.Artifact, workspacePath string, updateStore bool) (materializedArtifact, error) {
	workspaceDir, err := filepath.Abs(strings.TrimSpace(workspacePath))
	if err != nil {
		return materializedArtifact{}, err
	}
	if err := os.MkdirAll(filepath.Join(workspaceDir, ".tabura"), 0o755); err != nil {
		return materializedArtifact{}, err
	}

	result := materializedArtifact{
		Artifact:     artifact,
		OriginalPath: strings.TrimSpace(pointerString(artifact.RefPath)),
	}
	if refPath := strings.TrimSpace(pointerString(artifact.RefPath)); refPath != "" {
		absRef, err := filepath.Abs(refPath)
		if err == nil && pathWithinDir(absRef, workspaceDir) && fileExists(absRef) {
			rel, relErr := filepath.Rel(workspaceDir, absRef)
			if relErr != nil {
				return materializedArtifact{}, relErr
			}
			result.AbsolutePath = absRef
			result.RelativePath = filepath.ToSlash(rel)
			result.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(absRef)), ".")
			return result, nil
		}
		if fileExists(refPath) {
			absPath, relPath, copyErr := copyArtifactRefIntoWorkspace(artifact, workspaceDir, refPath)
			if copyErr != nil {
				return materializedArtifact{}, copyErr
			}
			result.AbsolutePath = absPath
			result.RelativePath = relPath
			result.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(absPath)), ".")
			result.CopiedFromRef = true
			if updateStore {
				if err := st.UpdateArtifact(artifact.ID, store.ArtifactUpdate{RefPath: &absPath}); err != nil {
					return materializedArtifact{}, err
				}
				result.Artifact.RefPath = &absPath
				result.StoreUpdated = true
			}
			return result, nil
		}
	}

	rendered, format, err := renderArtifactContent(artifact)
	if err != nil {
		return materializedArtifact{}, err
	}
	absPath, relPath, err := writeMaterializedArtifact(workspaceDir, artifact, format, rendered)
	if err != nil {
		return materializedArtifact{}, err
	}
	result.AbsolutePath = absPath
	result.RelativePath = relPath
	result.Format = format
	result.Materialized = true
	if updateStore {
		if err := st.UpdateArtifact(artifact.ID, store.ArtifactUpdate{RefPath: &absPath}); err != nil {
			return materializedArtifact{}, err
		}
		result.Artifact.RefPath = &absPath
		result.StoreUpdated = true
	}
	return result, nil
}

func copyArtifactRefIntoWorkspace(artifact store.Artifact, workspaceDir, refPath string) (string, string, error) {
	content, err := os.ReadFile(refPath)
	if err != nil {
		return "", "", err
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(refPath)), ".")
	if ext == "" {
		ext = "bin"
	}
	absPath, relPath, err := writeMaterializedArtifact(workspaceDir, artifact, ext, content)
	if err != nil {
		return "", "", err
	}
	return absPath, relPath, nil
}

func renderArtifactContent(artifact store.Artifact) ([]byte, string, error) {
	meta, err := decodeArtifactMeta(artifact.MetaJSON)
	if err != nil {
		return nil, "", err
	}
	switch artifact.Kind {
	case store.ArtifactKindEmail:
		return []byte(renderEmailEML(artifact, meta)), "eml", nil
	case store.ArtifactKind("calendar_event"):
		return []byte(renderCalendarICS(artifact, meta)), "ics", nil
	default:
		return []byte(renderArtifactMarkdown(artifact, meta)), "md", nil
	}
}

func renderEmailEML(artifact store.Artifact, meta map[string]any) string {
	var b strings.Builder
	subject := firstNonEmpty(metaString(meta, "subject"), pointerString(artifact.Title), fmt.Sprintf("Artifact %d", artifact.ID))
	from := firstNonEmpty(metaString(meta, "sender"), "unknown@local")
	to := strings.Join(metaStringSlice(meta, "recipients"), ", ")
	if to == "" {
		to = "undisclosed-recipients:;"
	}
	date := metaString(meta, "date")
	if date == "" {
		date = time.Now().UTC().Format(time.RFC3339)
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Date: %s\r\n", date)
	fmt.Fprintf(&b, "X-Tabura-Artifact-ID: %d\r\n", artifact.ID)
	if artifact.RefURL != nil && strings.TrimSpace(*artifact.RefURL) != "" {
		fmt.Fprintf(&b, "X-Tabura-Source-URL: %s\r\n", strings.TrimSpace(*artifact.RefURL))
	}
	b.WriteString("\r\n")
	body := firstNonEmpty(metaString(meta, "body"), metaString(meta, "snippet"))
	if body == "" {
		body = subject
	}
	b.WriteString(normalizeEMLBody(body))
	return b.String()
}

func renderCalendarICS(artifact store.Artifact, meta map[string]any) string {
	startRaw := firstNonEmpty(metaString(meta, "start"), metaString(meta, "starts_at"))
	endRaw := firstNonEmpty(metaString(meta, "end"), metaString(meta, "ends_at"))
	summary := firstNonEmpty(metaString(meta, "summary"), pointerString(artifact.Title), fmt.Sprintf("Artifact %d", artifact.ID))
	description := firstNonEmpty(metaString(meta, "description"), metaString(meta, "body"), metaString(meta, "content"))
	location := metaString(meta, "location")
	allDay := metaBool(meta, "all_day")
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//Tabura//Archive//EN\r\n")
	b.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(&b, "UID:tabura-artifact-%d@local\r\n", artifact.ID)
	fmt.Fprintf(&b, "DTSTAMP:%s\r\n", formatICSDateTime(artifact.UpdatedAt))
	if startRaw != "" {
		fmt.Fprintf(&b, "DTSTART%s:%s\r\n", icsDateParam(allDay), formatICSValue(startRaw, allDay))
	}
	if endRaw != "" {
		fmt.Fprintf(&b, "DTEND%s:%s\r\n", icsDateParam(allDay), formatICSValue(endRaw, allDay))
	}
	fmt.Fprintf(&b, "SUMMARY:%s\r\n", escapeICS(summary))
	if description != "" {
		fmt.Fprintf(&b, "DESCRIPTION:%s\r\n", escapeICS(description))
	}
	if location != "" {
		fmt.Fprintf(&b, "LOCATION:%s\r\n", escapeICS(location))
	}
	if artifact.RefURL != nil && strings.TrimSpace(*artifact.RefURL) != "" {
		fmt.Fprintf(&b, "URL:%s\r\n", escapeICS(strings.TrimSpace(*artifact.RefURL)))
	}
	b.WriteString("END:VEVENT\r\n")
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

func renderArtifactMarkdown(artifact store.Artifact, meta map[string]any) string {
	title := firstNonEmpty(pointerString(artifact.Title), metaString(meta, "subject"), metaString(meta, "summary"), fmt.Sprintf("Artifact %d", artifact.ID))
	lines := []string{
		"# " + title,
		"",
		fmt.Sprintf("- Artifact ID: %d", artifact.ID),
		fmt.Sprintf("- Kind: %s", artifact.Kind),
	}
	if artifact.RefURL != nil && strings.TrimSpace(*artifact.RefURL) != "" {
		lines = append(lines, fmt.Sprintf("- Source URL: %s", strings.TrimSpace(*artifact.RefURL)))
	}
	for _, key := range []string{"state", "owner_repo", "number", "project", "project_id", "section", "section_id", "priority"} {
		if value := metaScalarString(meta, key); value != "" {
			lines = append(lines, fmt.Sprintf("- %s: %s", humanizeKey(key), value))
		}
	}
	if labels := metaStringSlice(meta, "labels"); len(labels) > 0 {
		lines = append(lines, fmt.Sprintf("- Labels: %s", strings.Join(labels, ", ")))
	}
	if assignees := metaStringSlice(meta, "assignees"); len(assignees) > 0 {
		lines = append(lines, fmt.Sprintf("- Assignees: %s", strings.Join(assignees, ", ")))
	}
	if recipients := metaStringSlice(meta, "recipients"); len(recipients) > 0 {
		lines = append(lines, fmt.Sprintf("- Recipients: %s", strings.Join(recipients, ", ")))
	}
	if participants := metaStringSlice(meta, "participants"); len(participants) > 0 {
		lines = append(lines, fmt.Sprintf("- Participants: %s", strings.Join(participants, ", ")))
	}
	lines = append(lines, "")
	if body := firstNonEmpty(metaString(meta, "body"), metaString(meta, "description"), metaString(meta, "content"), metaString(meta, "snippet")); body != "" {
		lines = append(lines, body, "")
	}
	if messages, ok := meta["messages"].([]any); ok && len(messages) > 0 {
		lines = append(lines, "## Messages", "")
		for _, raw := range messages {
			record, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			subject := firstNonEmpty(metaAnyString(record, "subject"), "(untitled)")
			sender := metaAnyString(record, "sender")
			lines = append(lines, "### "+subject)
			if sender != "" {
				lines = append(lines, fmt.Sprintf("- Sender: %s", sender))
			}
			if date := metaAnyString(record, "date"); date != "" {
				lines = append(lines, fmt.Sprintf("- Date: %s", date))
			}
			if body := firstNonEmpty(metaAnyString(record, "body"), metaAnyString(record, "snippet")); body != "" {
				lines = append(lines, "", body)
			}
			lines = append(lines, "")
		}
	}
	if comments, ok := meta["comments"].([]any); ok && len(comments) > 0 {
		lines = append(lines, "## Comments", "")
		for _, raw := range comments {
			record, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			content := strings.TrimSpace(metaAnyString(record, "content"))
			if content == "" {
				continue
			}
			postedAt := metaAnyString(record, "posted_at")
			if postedAt != "" {
				lines = append(lines, fmt.Sprintf("- %s: %s", postedAt, content))
			} else {
				lines = append(lines, "- "+content)
			}
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func writeMaterializedArtifact(workspaceDir string, artifact store.Artifact, format string, content []byte) (string, string, error) {
	ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	if ext == "" {
		ext = "txt"
	}
	base := materializedFileBaseName(artifact)
	rel := filepath.ToSlash(filepath.Join(materializedRootDir, sanitizePathSegment(string(artifact.Kind)), base+"."+ext))
	abs := filepath.Join(workspaceDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		return "", "", err
	}
	return abs, rel, nil
}

func materializedFileBaseName(artifact store.Artifact) string {
	stem := strings.TrimSpace(pointerString(artifact.Title))
	if stem == "" && artifact.RefURL != nil {
		stem = filepath.Base(strings.TrimSpace(*artifact.RefURL))
	}
	if stem == "" && artifact.RefPath != nil {
		stem = filepath.Base(strings.TrimSpace(*artifact.RefPath))
	}
	stem = strings.TrimSuffix(stem, filepath.Ext(stem))
	stem = sanitizePathSegment(stem)
	if stem == "" {
		stem = sanitizePathSegment(string(artifact.Kind))
	}
	if stem == "" {
		stem = "artifact"
	}
	return fmt.Sprintf("%s-%d", stem, artifact.ID)
}

func decodeArtifactMeta(raw *string) (map[string]any, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return map[string]any{}, nil
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(*raw)), &meta); err != nil {
		return nil, fmt.Errorf("decode artifact meta: %w", err)
	}
	return meta, nil
}

func metaLabels(raw *string) []string {
	meta, err := decodeArtifactMeta(raw)
	if err != nil {
		return nil
	}
	return metaStringSlice(meta, "labels")
}

func metaString(meta map[string]any, key string) string {
	return metaAnyString(meta, key)
}

func metaAnyString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func metaScalarString(meta map[string]any, key string) string {
	value := metaAnyString(meta, key)
	if value == "false" {
		return ""
	}
	return value
}

func metaStringSlice(meta map[string]any, key string) []string {
	if meta == nil {
		return nil
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if clean := strings.TrimSpace(value); clean != "" {
				out = append(out, clean)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if clean := strings.TrimSpace(fmt.Sprint(value)); clean != "" {
				out = append(out, clean)
			}
		}
		return out
	default:
		if clean := strings.TrimSpace(fmt.Sprint(raw)); clean != "" {
			return []string{clean}
		}
		return nil
	}
}

func metaBool(meta map[string]any, key string) bool {
	if meta == nil {
		return false
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return false
	}
	switch typed := raw.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func pathWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func sanitizePathSegment(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	lastDash := false
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	clean := strings.Trim(b.String(), "-")
	if clean == "" {
		return "artifact"
	}
	return clean
}

func nullableString(raw string) *string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil
	}
	return &clean
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}

func normalizeEMLBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	return strings.ReplaceAll(body, "\n", "\r\n")
}

func formatICSDateTime(raw string) string {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Now().UTC().Format("20060102T150405Z")
	}
	return parsed.UTC().Format("20060102T150405Z")
}

func icsDateParam(allDay bool) string {
	if allDay {
		return ";VALUE=DATE"
	}
	return ""
}

func formatICSValue(raw string, allDay bool) string {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		if allDay && len(strings.TrimSpace(raw)) >= len("2006-01-02") {
			return strings.ReplaceAll(strings.TrimSpace(raw[:10]), "-", "")
		}
		return escapeICS(raw)
	}
	if allDay {
		return parsed.UTC().Format("20060102")
	}
	return parsed.UTC().Format("20060102T150405Z")
}

func escapeICS(raw string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		";", "\\;",
		",", "\\,",
		"\r\n", "\\n",
		"\n", "\\n",
		"\r", "\\n",
	)
	return replacer.Replace(strings.TrimSpace(raw))
}

func humanizeKey(key string) string {
	clean := strings.ReplaceAll(strings.TrimSpace(key), "_", " ")
	if clean == "" {
		return ""
	}
	return strings.ToUpper(clean[:1]) + clean[1:]
}
