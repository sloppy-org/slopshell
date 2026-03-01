package extensions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultExtensionTimeout = 1200 * time.Millisecond
	maxExtensionResponse    = 1 << 20
)

type Options struct {
	Dir            string
	RuntimeVersion string
	Logf           func(format string, args ...interface{})
}

type extensionResponse struct {
	Text           *string                 `json:"text"`
	Blocked        bool                    `json:"blocked,omitempty"`
	Reason         string                  `json:"reason,omitempty"`
	Decision       string                  `json:"decision,omitempty"`
	ResponseText   string                  `json:"response_text,omitempty"`
	Channel        string                  `json:"channel,omitempty"`
	Urgency        string                  `json:"urgency,omitempty"`
	Action         map[string]interface{}  `json:"action,omitempty"`
	MeetingPartner *MeetingPartnerDecision `json:"meeting_partner,omitempty"`
	Command        *CommandResult          `json:"command,omitempty"`
}

type runtimeCommand struct {
	info CommandInfo
}

type runtimeExtension struct {
	info      ExtensionInfo
	hookSet   map[string]struct{}
	permSet   map[string]struct{}
	commands  map[string]runtimeCommand
	timeout   time.Duration
	secretEnv string
}

type Host struct {
	extensions []runtimeExtension
	client     *http.Client
	logf       func(format string, args ...interface{})
}

func New(opts Options) (*Host, error) {
	h := &Host{
		client: &http.Client{Timeout: 0},
		logf:   opts.Logf,
	}
	if h.logf == nil {
		h.logf = func(string, ...interface{}) {}
	}
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		return h, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return h, nil
		}
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || !strings.HasSuffix(strings.ToLower(name), ".extension.json") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	for _, file := range files {
		ext, err := loadManifestFile(file, strings.TrimSpace(opts.RuntimeVersion))
		if err != nil {
			return nil, err
		}
		if !ext.info.Enabled {
			continue
		}
		h.extensions = append(h.extensions, ext)
	}
	return h, nil
}

func (h *Host) Count() int {
	if h == nil {
		return 0
	}
	return len(h.extensions)
}

func (h *Host) List() []ExtensionInfo {
	if h == nil || len(h.extensions) == 0 {
		return nil
	}
	out := make([]ExtensionInfo, 0, len(h.extensions))
	for _, ext := range h.extensions {
		info := ext.info
		info.Hooks = append([]string(nil), info.Hooks...)
		info.Permissions = append([]string(nil), info.Permissions...)
		info.Commands = append([]CommandInfo(nil), info.Commands...)
		info.UIContributions = append([]UIContributionInfo(nil), info.UIContributions...)
		out = append(out, info)
	}
	return out
}

func (h *Host) Apply(ctx context.Context, req HookRequest) HookResult {
	text := req.Text
	if h == nil || len(h.extensions) == 0 {
		return HookResult{Text: text}
	}
	hook := strings.TrimSpace(req.Hook)
	if hook == "" {
		return HookResult{Text: text}
	}
	for _, ext := range h.extensions {
		if _, ok := ext.hookSet[hook]; !ok {
			continue
		}
		if !ext.allowedForHook(hook) {
			h.logf("extension %q skipped hook %q due to missing permission", ext.info.ID, hook)
			continue
		}
		callReq := req
		callReq.Text = text
		resp, err := h.call(ctx, ext, callReq)
		if err != nil {
			h.logf("extension %q hook %q failed: %v", ext.info.ID, hook, err)
			continue
		}
		if resp.Blocked {
			reason := strings.TrimSpace(resp.Reason)
			if reason == "" {
				reason = "extension blocked request"
			}
			return HookResult{Text: text, Blocked: true, Reason: reason}
		}
		if resp.Text != nil {
			text = *resp.Text
		}
	}
	return HookResult{Text: text}
}

func (h *Host) DecideMeetingPartner(ctx context.Context, req HookRequest) (MeetingPartnerDecision, bool) {
	if h == nil || len(h.extensions) == 0 {
		return MeetingPartnerDecision{}, false
	}
	hook := strings.TrimSpace(req.Hook)
	if hook == "" {
		hook = HookMeetingPartnerDecide
	}
	req.Hook = hook
	for _, ext := range h.extensions {
		if _, ok := ext.hookSet[hook]; !ok {
			continue
		}
		if !ext.allowedForHook(hook) {
			continue
		}
		resp, err := h.call(ctx, ext, req)
		if err != nil {
			h.logf("extension %q hook %q failed: %v", ext.info.ID, hook, err)
			continue
		}
		if decision, ok := decodeMeetingPartnerDecision(resp); ok {
			decision.PluginID = ext.info.ID
			return decision, true
		}
	}
	return MeetingPartnerDecision{}, false
}

func (h *Host) ExecuteCommand(ctx context.Context, req CommandRequest) (CommandResult, error) {
	if h == nil {
		return CommandResult{}, errors.New("extension host is not initialized")
	}
	commandID := strings.TrimSpace(req.CommandID)
	if commandID == "" {
		return CommandResult{}, errors.New("command_id is required")
	}
	ext, cmd, ok := h.findCommand(commandID)
	if !ok {
		return CommandResult{}, fmt.Errorf("command %q not found", commandID)
	}
	if _, allowed := ext.permSet[cmd.info.Permission]; !allowed {
		return CommandResult{}, fmt.Errorf("extension %q lacks %q permission", ext.info.ID, cmd.info.Permission)
	}
	metadata := copyMap(req.Metadata)
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metadata["command_id"] = commandID
	if len(req.Args) > 0 {
		metadata["args"] = req.Args
	}
	resp, err := h.call(ctx, ext, HookRequest{
		Hook:       cmd.info.Hook,
		SessionID:  strings.TrimSpace(req.SessionID),
		ProjectKey: strings.TrimSpace(req.ProjectKey),
		OutputMode: strings.TrimSpace(req.OutputMode),
		Text:       strings.TrimSpace(req.Text),
		Metadata:   metadata,
	})
	if err != nil {
		return CommandResult{}, err
	}
	if resp.Blocked {
		reason := strings.TrimSpace(resp.Reason)
		if reason == "" {
			reason = "command blocked by extension"
		}
		return CommandResult{}, errors.New(reason)
	}
	if resp.Command != nil {
		result := *resp.Command
		if strings.TrimSpace(result.CommandID) == "" {
			result.CommandID = commandID
		}
		if strings.TrimSpace(result.ExtensionID) == "" {
			result.ExtensionID = ext.info.ID
		}
		return result, nil
	}
	message := strings.TrimSpace(req.Text)
	if resp.Text != nil {
		message = strings.TrimSpace(*resp.Text)
	}
	return CommandResult{
		CommandID:   commandID,
		ExtensionID: ext.info.ID,
		Success:     true,
		Message:     message,
	}, nil
}

func (h *Host) findCommand(commandID string) (runtimeExtension, runtimeCommand, bool) {
	for _, ext := range h.extensions {
		cmd, ok := ext.commands[commandID]
		if ok {
			return ext, cmd, true
		}
	}
	return runtimeExtension{}, runtimeCommand{}, false
}

func (h *Host) call(parent context.Context, ext runtimeExtension, req HookRequest) (extensionResponse, error) {
	var out extensionResponse
	payload, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	ctx := parent
	cancel := func() {}
	if ext.timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, ext.timeout)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ext.info.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Tabura-Extension-ID", ext.info.ID)
	if secret := strings.TrimSpace(os.Getenv(ext.secretEnv)); secret != "" {
		httpReq.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := h.client.Do(httpReq)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return out, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxExtensionResponse))
	if err := dec.Decode(&out); err != nil && !errors.Is(err, io.EOF) {
		return extensionResponse{}, err
	}
	return out, nil
}

func (ext runtimeExtension) allowedForHook(hook string) bool {
	required := requiredPermissionForHook(hook)
	if strings.TrimSpace(required) == "" {
		return true
	}
	if _, ok := ext.permSet[required]; ok {
		return true
	}
	_, ok := ext.permSet[PermissionHookExecute]
	return ok
}

func loadManifestFile(path, runtimeVersion string) (runtimeExtension, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return runtimeExtension{}, err
	}
	var cfg Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return runtimeExtension{}, fmt.Errorf("%s: %w", path, err)
	}
	ext, err := compileManifest(cfg, runtimeVersion)
	if err != nil {
		return runtimeExtension{}, fmt.Errorf("%s: %w", path, err)
	}
	return ext, nil
}

func compileManifest(cfg Manifest, runtimeVersion string) (runtimeExtension, error) {
	id := strings.TrimSpace(cfg.ID)
	if id == "" {
		return runtimeExtension{}, errors.New("id is required")
	}
	version := strings.TrimSpace(cfg.Version)
	if version == "" {
		version = "0.1.0"
	}
	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	if kind == "" {
		kind = "webhook"
	}
	if kind != "webhook" {
		return runtimeExtension{}, fmt.Errorf("unsupported kind %q", kind)
	}
	if err := ensureEngineCompatible(cfg.Engine, runtimeVersion); err != nil {
		return runtimeExtension{}, err
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return runtimeExtension{}, errors.New("endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return runtimeExtension{}, fmt.Errorf("invalid endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return runtimeExtension{}, errors.New("endpoint scheme must be http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return runtimeExtension{}, errors.New("endpoint host is required")
	}
	hooks := normalizeStrings(cfg.Hooks)
	commands, commandMap, err := normalizeCommands(cfg.Commands)
	if err != nil {
		return runtimeExtension{}, err
	}
	uiContributions, err := normalizeUIContributions(cfg.UIContributions)
	if err != nil {
		return runtimeExtension{}, err
	}
	if len(hooks) == 0 && len(commands) == 0 && len(uiContributions) == 0 {
		return runtimeExtension{}, errors.New("at least one hook, command, or ui_contribution is required")
	}
	permissions := normalizePermissions(cfg.Permissions)
	permissions = derivePermissions(permissions, hooks, commands, uiContributions)
	permSet := make(map[string]struct{}, len(permissions))
	for _, perm := range permissions {
		permSet[perm] = struct{}{}
	}
	timeoutMS := cfg.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = int(defaultExtensionTimeout / time.Millisecond)
	}
	if timeoutMS > 30000 {
		timeoutMS = 30000
	}
	info := ExtensionInfo{
		ID:              id,
		DisplayName:     strings.TrimSpace(cfg.DisplayName),
		Version:         version,
		Kind:            kind,
		Endpoint:        endpoint,
		Hooks:           hooks,
		Permissions:     permissions,
		Commands:        commands,
		UIContributions: uiContributions,
		Enabled:         cfg.Enabled,
		TimeoutMS:       timeoutMS,
		Engine:          cfg.Engine,
		Signing:         cfg.Signing,
	}
	hookSet := make(map[string]struct{}, len(hooks))
	for _, hook := range hooks {
		hookSet[hook] = struct{}{}
	}
	return runtimeExtension{
		info:      info,
		hookSet:   hookSet,
		permSet:   permSet,
		commands:  commandMap,
		timeout:   time.Duration(timeoutMS) * time.Millisecond,
		secretEnv: strings.TrimSpace(cfg.SecretEnv),
	}, nil
}

func normalizeCommands(in []CommandManifest) ([]CommandInfo, map[string]runtimeCommand, error) {
	if len(in) == 0 {
		return nil, map[string]runtimeCommand{}, nil
	}
	out := make([]CommandInfo, 0, len(in))
	commandMap := make(map[string]runtimeCommand, len(in))
	for _, entry := range in {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			return nil, nil, errors.New("command id is required")
		}
		if _, exists := commandMap[id]; exists {
			return nil, nil, fmt.Errorf("duplicate command id %q", id)
		}
		hook := strings.TrimSpace(entry.Hook)
		if hook == "" {
			hook = HookExtensionCommand
		}
		permission := strings.ToLower(strings.TrimSpace(entry.Permission))
		if permission == "" {
			permission = PermissionCommandExecute
		}
		info := CommandInfo{
			ID:          id,
			Title:       strings.TrimSpace(entry.Title),
			Description: strings.TrimSpace(entry.Description),
			Hook:        hook,
			Permission:  permission,
		}
		out = append(out, info)
		commandMap[id] = runtimeCommand{info: info}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, commandMap, nil
}

func normalizeUIContributions(in []UIContributionManifest) ([]UIContributionInfo, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]UIContributionInfo, 0, len(in))
	for _, entry := range in {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			return nil, errors.New("ui contribution id is required")
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("duplicate ui contribution id %q", id)
		}
		seen[id] = struct{}{}
		slot := strings.TrimSpace(entry.Slot)
		if slot == "" {
			return nil, fmt.Errorf("ui contribution %q slot is required", id)
		}
		out = append(out, UIContributionInfo{
			ID:          id,
			Slot:        slot,
			Title:       strings.TrimSpace(entry.Title),
			Description: strings.TrimSpace(entry.Description),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func normalizeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizePermissions(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		perm := strings.ToLower(strings.TrimSpace(raw))
		if perm == "" {
			continue
		}
		if _, ok := seen[perm]; ok {
			continue
		}
		seen[perm] = struct{}{}
		out = append(out, perm)
	}
	sort.Strings(out)
	return out
}

func derivePermissions(existing []string, hooks []string, commands []CommandInfo, ui []UIContributionInfo) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(existing)+len(hooks)+len(commands)+len(ui)+1)
	for _, perm := range existing {
		seen[perm] = struct{}{}
		out = append(out, perm)
	}
	for _, hook := range hooks {
		required := requiredPermissionForHook(hook)
		if strings.TrimSpace(required) == "" {
			continue
		}
		if _, ok := seen[required]; ok {
			continue
		}
		seen[required] = struct{}{}
		out = append(out, required)
	}
	for _, cmd := range commands {
		if _, ok := seen[cmd.Permission]; !ok {
			seen[cmd.Permission] = struct{}{}
			out = append(out, cmd.Permission)
		}
	}
	if len(ui) > 0 {
		if _, ok := seen[PermissionUIContribute]; !ok {
			seen[PermissionUIContribute] = struct{}{}
			out = append(out, PermissionUIContribute)
		}
	}
	sort.Strings(out)
	return out
}

func requiredPermissionForHook(hook string) string {
	switch strings.TrimSpace(hook) {
	case HookChatPreUserMessage:
		return PermissionHookChatPreUserMessage
	case HookChatPreAssistantPrompt:
		return PermissionHookChatPreAssistantPrompt
	case HookChatPostAssistantReply:
		return PermissionHookChatPostAssistantReply
	case HookMeetingPartnerSession:
		return PermissionMeetingPartnerSession
	case HookMeetingPartnerSegment:
		return PermissionMeetingPartnerSegment
	case HookMeetingPartnerDecide:
		return PermissionMeetingPartnerDecide
	default:
		return PermissionHookExecute
	}
}

func ensureEngineCompatible(engine EngineManifest, runtimeVersion string) error {
	constraint := strings.TrimSpace(engine.Tabura)
	if constraint == "" || strings.TrimSpace(runtimeVersion) == "" {
		return nil
	}
	ok, err := satisfiesConstraint(runtimeVersion, constraint)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("engine.tabura %q is not compatible with runtime %q", constraint, runtimeVersion)
	}
	return nil
}

func satisfiesConstraint(version, constraint string) (bool, error) {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return true, nil
	}
	if strings.HasPrefix(constraint, ">=") {
		base := strings.TrimSpace(strings.TrimPrefix(constraint, ">="))
		cmp, err := compareSemver(version, base)
		if err != nil {
			return false, err
		}
		return cmp >= 0, nil
	}
	cmp, err := compareSemver(version, constraint)
	if err != nil {
		return false, err
	}
	return cmp == 0, nil
}

func compareSemver(a, b string) (int, error) {
	av, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		if av[i] < bv[i] {
			return -1, nil
		}
		if av[i] > bv[i] {
			return 1, nil
		}
	}
	return 0, nil
}

func parseSemver(raw string) ([3]int, error) {
	var out [3]int
	value := strings.TrimSpace(raw)
	if value == "" {
		return out, errors.New("empty semver")
	}
	if idx := strings.IndexAny(value, "-+"); idx >= 0 {
		value = value[:idx]
	}
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return out, fmt.Errorf("invalid semver %q", raw)
	}
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			out[i] = 0
			continue
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return out, fmt.Errorf("invalid semver %q", raw)
		}
		out[i] = n
	}
	return out, nil
}

func decodeMeetingPartnerDecision(resp extensionResponse) (MeetingPartnerDecision, bool) {
	if resp.MeetingPartner != nil {
		decision := normalizeMeetingPartnerDecision(*resp.MeetingPartner)
		if decision.Decision != "" {
			return decision, true
		}
	}
	decision := normalizeMeetingPartnerDecision(MeetingPartnerDecision{
		Decision:     resp.Decision,
		ResponseText: resp.ResponseText,
		Channel:      resp.Channel,
		Urgency:      resp.Urgency,
		Action:       resp.Action,
		Reason:       resp.Reason,
	})
	if decision.Decision == "" {
		return MeetingPartnerDecision{}, false
	}
	return decision, true
}

func normalizeMeetingPartnerDecision(in MeetingPartnerDecision) MeetingPartnerDecision {
	in.Decision = strings.ToLower(strings.TrimSpace(in.Decision))
	in.ResponseText = strings.TrimSpace(in.ResponseText)
	in.Channel = strings.ToLower(strings.TrimSpace(in.Channel))
	in.Urgency = strings.ToLower(strings.TrimSpace(in.Urgency))
	in.Reason = strings.TrimSpace(in.Reason)
	switch in.Decision {
	case "noop":
		return in
	case "respond":
		if in.Channel == "" {
			in.Channel = "voice"
		}
		if in.Urgency == "" {
			in.Urgency = "normal"
		}
		return in
	case "action":
		if in.Action == nil {
			in.Action = map[string]interface{}{}
		}
		return in
	default:
		return MeetingPartnerDecision{}
	}
}

func copyMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
