package plugins

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
	"strings"
	"time"
)

const (
	HookChatPreUserMessage     = "chat.pre_user_message"
	HookChatPreAssistantPrompt = "chat.pre_assistant_prompt"
	HookChatPostAssistantReply = "chat.post_assistant_response"
	HookMeetingPartnerSession  = "meeting_partner.session_state"
	HookMeetingPartnerSegment  = "meeting_partner.segment_finalized"
	HookMeetingPartnerDecide   = "meeting_partner.decide"
)

const (
	defaultPluginTimeout = 1200 * time.Millisecond
	maxPluginResponse    = 1 << 20
)

type Options struct {
	Dir  string
	Logf func(format string, args ...interface{})
}

type HookRequest struct {
	Hook       string                 `json:"hook"`
	SessionID  string                 `json:"session_id,omitempty"`
	ProjectKey string                 `json:"project_key,omitempty"`
	OutputMode string                 `json:"output_mode,omitempty"`
	Text       string                 `json:"text,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type HookResult struct {
	Text    string
	Blocked bool
	Reason  string
}

type MeetingPartnerDecision struct {
	Decision     string                 `json:"decision"`
	ResponseText string                 `json:"response_text,omitempty"`
	Channel      string                 `json:"channel,omitempty"`
	Urgency      string                 `json:"urgency,omitempty"`
	Action       map[string]interface{} `json:"action,omitempty"`
	Reason       string                 `json:"reason,omitempty"`
	PluginID     string                 `json:"plugin_id,omitempty"`
}

type PluginInfo struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Endpoint  string   `json:"endpoint"`
	Hooks     []string `json:"hooks"`
	TimeoutMS int      `json:"timeout_ms"`
	Enabled   bool     `json:"enabled"`
}

type manifest struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Endpoint  string   `json:"endpoint"`
	Hooks     []string `json:"hooks"`
	TimeoutMS int      `json:"timeout_ms"`
	Enabled   bool     `json:"enabled"`
	SecretEnv string   `json:"secret_env,omitempty"`
}

type hookResponse struct {
	Text           *string                 `json:"text"`
	Blocked        bool                    `json:"blocked,omitempty"`
	Reason         string                  `json:"reason,omitempty"`
	Decision       string                  `json:"decision,omitempty"`
	ResponseText   string                  `json:"response_text,omitempty"`
	Channel        string                  `json:"channel,omitempty"`
	Urgency        string                  `json:"urgency,omitempty"`
	Action         map[string]interface{}  `json:"action,omitempty"`
	MeetingPartner *MeetingPartnerDecision `json:"meeting_partner,omitempty"`
}

type runtimePlugin struct {
	info      PluginInfo
	hookSet   map[string]struct{}
	timeout   time.Duration
	secretEnv string
}

type Manager struct {
	plugins []runtimePlugin
	client  *http.Client
	logf    func(format string, args ...interface{})
}

func New(opts Options) (*Manager, error) {
	m := &Manager{
		client: &http.Client{Timeout: 0},
		logf:   opts.Logf,
	}
	if m.logf == nil {
		m.logf = func(string, ...interface{}) {}
	}
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		return m, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, nil
		}
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if !isPluginManifestFileName(name) {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	for _, file := range files {
		plug, err := loadManifestFile(file)
		if err != nil {
			return nil, err
		}
		if !plug.info.Enabled {
			continue
		}
		m.plugins = append(m.plugins, plug)
	}
	return m, nil
}

func (m *Manager) Count() int {
	if m == nil {
		return 0
	}
	return len(m.plugins)
}

func (m *Manager) List() []PluginInfo {
	if m == nil || len(m.plugins) == 0 {
		return nil
	}
	out := make([]PluginInfo, 0, len(m.plugins))
	for _, plug := range m.plugins {
		info := plug.info
		info.Hooks = append([]string(nil), info.Hooks...)
		out = append(out, info)
	}
	return out
}

func (m *Manager) Apply(ctx context.Context, req HookRequest) HookResult {
	text := req.Text
	if m == nil || len(m.plugins) == 0 {
		return HookResult{Text: text}
	}
	hook := strings.TrimSpace(req.Hook)
	if hook == "" {
		return HookResult{Text: text}
	}
	for _, plug := range m.plugins {
		if _, ok := plug.hookSet[hook]; !ok {
			continue
		}
		callReq := req
		callReq.Text = text
		resp, err := m.call(ctx, plug, callReq)
		if err != nil {
			m.logf("plugin %q hook %q failed: %v", plug.info.ID, hook, err)
			continue
		}
		if resp.Blocked {
			reason := strings.TrimSpace(resp.Reason)
			if reason == "" {
				reason = "plugin blocked request"
			}
			return HookResult{Text: text, Blocked: true, Reason: reason}
		}
		if resp.Text != nil {
			text = *resp.Text
		}
	}
	return HookResult{Text: text}
}

func (m *Manager) DecideMeetingPartner(ctx context.Context, req HookRequest) (MeetingPartnerDecision, bool) {
	if m == nil || len(m.plugins) == 0 {
		return MeetingPartnerDecision{}, false
	}
	hook := strings.TrimSpace(req.Hook)
	if hook == "" {
		hook = HookMeetingPartnerDecide
	}
	req.Hook = hook
	for _, plug := range m.plugins {
		if _, ok := plug.hookSet[hook]; !ok {
			continue
		}
		resp, err := m.call(ctx, plug, req)
		if err != nil {
			m.logf("plugin %q hook %q failed: %v", plug.info.ID, hook, err)
			continue
		}
		if decision, ok := decodeMeetingPartnerDecision(resp); ok {
			decision.PluginID = plug.info.ID
			return decision, true
		}
	}
	return MeetingPartnerDecision{}, false
}

func (m *Manager) call(parent context.Context, plug runtimePlugin, req HookRequest) (hookResponse, error) {
	var out hookResponse
	payload, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	ctx := parent
	cancel := func() {}
	if plug.timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, plug.timeout)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, plug.info.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Tabura-Plugin-ID", plug.info.ID)
	if secret := strings.TrimSpace(os.Getenv(plug.secretEnv)); secret != "" {
		httpReq.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := m.client.Do(httpReq)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return out, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxPluginResponse))
	if err := dec.Decode(&out); err != nil && !errors.Is(err, io.EOF) {
		return hookResponse{}, err
	}
	return out, nil
}

func loadManifestFile(path string) (runtimePlugin, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return runtimePlugin{}, err
	}
	var cfg manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return runtimePlugin{}, fmt.Errorf("%s: %w", path, err)
	}
	plug, err := compileManifest(cfg)
	if err != nil {
		return runtimePlugin{}, fmt.Errorf("%s: %w", path, err)
	}
	return plug, nil
}

func compileManifest(cfg manifest) (runtimePlugin, error) {
	id := strings.TrimSpace(cfg.ID)
	if id == "" {
		return runtimePlugin{}, errors.New("id is required")
	}
	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	if kind == "" {
		kind = "webhook"
	}
	if kind != "webhook" {
		return runtimePlugin{}, fmt.Errorf("unsupported kind %q", kind)
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return runtimePlugin{}, errors.New("endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return runtimePlugin{}, fmt.Errorf("invalid endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return runtimePlugin{}, errors.New("endpoint scheme must be http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return runtimePlugin{}, errors.New("endpoint host is required")
	}
	hooks := normalizeHooks(cfg.Hooks)
	if len(hooks) == 0 {
		return runtimePlugin{}, errors.New("at least one hook is required")
	}
	timeoutMS := cfg.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = int(defaultPluginTimeout / time.Millisecond)
	}
	if timeoutMS > 30000 {
		timeoutMS = 30000
	}
	info := PluginInfo{
		ID:        id,
		Kind:      kind,
		Endpoint:  endpoint,
		Hooks:     hooks,
		TimeoutMS: timeoutMS,
		Enabled:   cfg.Enabled,
	}
	hookSet := make(map[string]struct{}, len(hooks))
	for _, hook := range hooks {
		hookSet[hook] = struct{}{}
	}
	return runtimePlugin{
		info:      info,
		hookSet:   hookSet,
		timeout:   time.Duration(timeoutMS) * time.Millisecond,
		secretEnv: strings.TrimSpace(cfg.SecretEnv),
	}, nil
}

func isPluginManifestFileName(name string) bool {
	clean := strings.ToLower(strings.TrimSpace(name))
	if clean == "" || !strings.HasSuffix(clean, ".json") {
		return false
	}
	return !strings.HasSuffix(clean, ".extension.json")
}

func normalizeHooks(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		hook := strings.TrimSpace(raw)
		if hook == "" {
			continue
		}
		if _, ok := seen[hook]; ok {
			continue
		}
		seen[hook] = struct{}{}
		out = append(out, hook)
	}
	sort.Strings(out)
	return out
}

func decodeMeetingPartnerDecision(resp hookResponse) (MeetingPartnerDecision, bool) {
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
