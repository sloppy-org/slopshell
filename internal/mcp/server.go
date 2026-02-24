package mcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/canvas"
	"github.com/krystophny/tabura/internal/surface"
)

const (
	ServerName             = "tabura"
	ServerVersion          = "0.1.0"
	LatestProtocolVersion  = "2025-03-26"
	defaultProducerMCPURL  = "http://127.0.0.1:8090/mcp"
	handoffKindFile        = "file"
	delegateDefaultTimeout = 3600 * time.Second

	delegateStatusRunning   = "running"
	delegateStatusCompleted = "completed"
	delegateStatusFailed    = "failed"
	delegateStatusCanceled  = "canceled"

	delegateStatusDefaultMaxEvents = 20
	delegateStatusHardMaxEvents    = 100
	delegateEventHistoryLimit      = 256
	delegateFinishedJobRetention   = 24 * time.Hour
	tempArtifactsDirRel            = ".tabura/artifacts/tmp"
)

var supportedProtocolVersions = map[string]struct{}{
	"2024-11-05": {},
	"2025-03-26": {},
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Server struct {
	adapter         *canvas.Adapter
	appServerClient *appserver.Client

	delegateMu   sync.Mutex
	delegateJobs map[string]*delegateJob
}

type handoffEnvelope struct {
	SpecVersion string                 `json:"spec_version"`
	HandoffID   string                 `json:"handoff_id"`
	Kind        string                 `json:"kind"`
	CreatedAt   string                 `json:"created_at"`
	Meta        map[string]interface{} `json:"meta"`
	Payload     map[string]interface{} `json:"payload"`
}

type delegateJob struct {
	ID             string
	Status         string
	Model          string
	CWD            string
	TimeoutSeconds int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	FinishedAt     time.Time
	ThreadID       string
	TurnID         string
	Message        string
	FilesChanged   []string
	Error          string
	Events         []delegateProgressEvent
	NextSeq        int64
	Cancel         context.CancelFunc
}

type delegateProgressEvent struct {
	Seq  int64
	At   time.Time
	Type string
	Text string
}

func NewServer(adapter *canvas.Adapter, appServerClient ...*appserver.Client) *Server {
	var client *appserver.Client
	if len(appServerClient) > 0 {
		client = appServerClient[0]
	}
	return &Server{
		adapter:         adapter,
		appServerClient: client,
		delegateJobs:    make(map[string]*delegateJob),
	}
}

func (s *Server) DispatchMessage(message map[string]interface{}) map[string]interface{} {
	id, hasID := message["id"]
	method, _ := message["method"].(string)
	if strings.TrimSpace(method) == "" {
		if hasID {
			return rpcErr(id, -32600, "missing method")
		}
		return nil
	}
	if !hasID {
		return nil
	}
	params, _ := message["params"].(map[string]interface{})
	if params == nil {
		params = map[string]interface{}{}
	}

	result, rerr := s.dispatch(method, params)
	if rerr != nil {
		return map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": rerr}
	}
	return map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcErr(id interface{}, code int, message string) map[string]interface{} {
	return map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": RPCError{Code: code, Message: message}}
}

func (s *Server) dispatch(method string, params map[string]interface{}) (map[string]interface{}, *RPCError) {
	switch method {
	case "initialize":
		requested, _ := params["protocolVersion"].(string)
		v := LatestProtocolVersion
		if _, ok := supportedProtocolVersions[requested]; ok {
			v = requested
		}
		return map[string]interface{}{
			"protocolVersion": v,
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{"listChanged": false},
				"resources": map[string]interface{}{"subscribe": false},
			},
			"serverInfo": map[string]interface{}{"name": ServerName, "version": ServerVersion},
		}, nil
	case "ping":
		return map[string]interface{}{}, nil
	case "tools/list":
		return map[string]interface{}{"tools": toolDefinitions()}, nil
	case "resources/list":
		return map[string]interface{}{"resources": resourcesList(s.adapter)}, nil
	case "resources/templates/list":
		return map[string]interface{}{"resourceTemplates": resourceTemplates()}, nil
	case "resources/read":
		return s.dispatchResourceRead(params)
	case "tools/call":
		return s.dispatchToolCall(params)
	default:
		return nil, &RPCError{Code: -32601, Message: "method not found: " + method}
	}
}

func (s *Server) dispatchToolCall(params map[string]interface{}) (map[string]interface{}, *RPCError) {
	name, _ := params["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, &RPCError{Code: -32602, Message: "tools/call requires non-empty name"}
	}
	args, _ := params["arguments"].(map[string]interface{})
	if args == nil {
		args = map[string]interface{}{}
	}
	structured, err := s.callTool(name, args)
	if err != nil {
		return map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": err.Error()}},
			"isError": true,
		}, nil
	}
	b, _ := json.Marshal(structured)
	return map[string]interface{}{
		"content":           []map[string]string{{"type": "text", "text": string(b)}},
		"structuredContent": structured,
		"isError":           false,
	}, nil
}

func (s *Server) callTool(name string, args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	switch name {
	case "canvas_session_open", "canvas_activate":
		return s.adapter.CanvasSessionOpen(sid, strArg(args, "mode_hint")), nil
	case "canvas_artifact_show":
		text := strArg(args, "markdown_or_text")
		if text == "" {
			text = strArg(args, "text")
		}
		return s.adapter.CanvasArtifactShow(
			sid,
			strArg(args, "kind"),
			strArg(args, "title"),
			text,
			strArg(args, "path"),
			intArg(args, "page", 0),
			strArg(args, "reason"),
			nil,
		)
	case "canvas_render_text":
		text := strArg(args, "markdown_or_text")
		if text == "" {
			text = strArg(args, "text")
		}
		return s.adapter.CanvasArtifactShow(sid, "text", strArg(args, "title"), text, "", 0, "", nil)
	case "canvas_render_image":
		return s.adapter.CanvasArtifactShow(sid, "image", strArg(args, "title"), "", strArg(args, "path"), 0, "", nil)
	case "canvas_render_pdf":
		return s.adapter.CanvasArtifactShow(sid, "pdf", strArg(args, "title"), "", strArg(args, "path"), intArg(args, "page", 0), "", nil)
	case "canvas_clear":
		return s.adapter.CanvasArtifactShow(sid, "clear", "", "", "", 0, strArg(args, "reason"), nil)
	case "canvas_status":
		return s.adapter.CanvasStatus(sid), nil
	case "canvas_history":
		return s.adapter.CanvasHistory(sid, intArg(args, "limit", 20)), nil
	case "canvas_import_handoff":
		return s.canvasImportHandoff(sid, args)
	case "temp_file_create":
		return s.tempFileCreate(args)
	case "temp_file_remove":
		return s.tempFileRemove(args)
	case "delegate_to_model":
		return s.delegateToModel(args)
	case "delegate_to_model_status":
		return s.delegateToModelStatus(args)
	case "delegate_to_model_cancel":
		return s.delegateToModelCancel(args)
	case "delegate_to_model_active_count":
		return s.delegateToModelActiveCount(args)
	case "delegate_to_model_cancel_all":
		return s.delegateToModelCancelAll(args)
	default:
		return nil, errors.New("unknown tool: " + name)
	}
}

var modelAliases = map[string]string{
	"spark": "gpt-5.3-codex-spark",
	"codex": "gpt-5.3-codex",
	"gpt":   "gpt-5.2",
}

func resolveModelAlias(raw string) string {
	key := strings.TrimSpace(strings.ToLower(raw))
	if key == "" {
		return modelAliases["codex"]
	}
	if full, ok := modelAliases[key]; ok {
		return full
	}
	return raw
}

// delegateReasoningParams returns high reasoning effort for non-spark models
// (gpt-5.3-codex, gpt-5.2) so delegated tasks get full reasoning budget.
func delegateReasoningParams(model string) map[string]interface{} {
	m := strings.TrimSpace(strings.ToLower(model))
	if m == "" || strings.Contains(m, "spark") {
		return nil
	}
	return map[string]interface{}{"model_reasoning_effort": "high"}
}

const defaultDelegateSystemPrompt = "You have full filesystem access in the working directory. " +
	"Edit files directly using your tools. " +
	"Do NOT output patches or diffs for the caller to apply — make all changes yourself. " +
	"After completing the task, summarize what you did and which files you changed."

func assembleDelegatePrompt(systemPrompt, taskContext, prompt string) string {
	var b strings.Builder
	systemPrompt = strings.TrimSpace(systemPrompt)
	if systemPrompt == "" {
		systemPrompt = defaultDelegateSystemPrompt
	}
	b.WriteString("## Instructions\n\n")
	b.WriteString(systemPrompt)
	b.WriteString("\n\n")
	if taskContext = strings.TrimSpace(taskContext); taskContext != "" {
		b.WriteString("## Context\n\n")
		b.WriteString(taskContext)
		b.WriteString("\n\n")
	}
	b.WriteString("## Task\n\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

type delegateRequest struct {
	Model          string
	FullPrompt     string
	CWD            string
	TimeoutSeconds int
	Timeout        time.Duration
	Reasoning      map[string]interface{}
}

func (s *Server) buildDelegateRequest(args map[string]interface{}) (delegateRequest, error) {
	prompt := strings.TrimSpace(strArg(args, "prompt"))
	if prompt == "" {
		return delegateRequest{}, errors.New("prompt is required")
	}
	model := resolveModelAlias(strArg(args, "model"))
	fullPrompt := assembleDelegatePrompt(
		strArg(args, "system_prompt"),
		strArg(args, "context"),
		prompt,
	)
	cwd := strings.TrimSpace(strArg(args, "cwd"))
	if cwd == "" {
		cwd = s.adapter.ProjectDir()
	}
	timeoutSeconds := intArg(args, "timeout_seconds", 0)
	if timeoutSeconds <= 0 {
		timeoutSeconds = int(delegateDefaultTimeout.Seconds())
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	return delegateRequest{
		Model:          model,
		FullPrompt:     fullPrompt,
		CWD:            cwd,
		TimeoutSeconds: timeoutSeconds,
		Timeout:        timeout,
		Reasoning:      delegateReasoningParams(model),
	}, nil
}

func (s *Server) delegateToModel(args map[string]interface{}) (map[string]interface{}, error) {
	return s.startDelegateJob(args)
}

func (s *Server) startDelegateJob(args map[string]interface{}) (map[string]interface{}, error) {
	if s.appServerClient == nil {
		return nil, errors.New("delegate_to_model is unavailable: app-server client is not configured")
	}
	req, err := s.buildDelegateRequest(args)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), req.Timeout)
	job := &delegateJob{
		ID:             newDelegateJobID(),
		Status:         delegateStatusRunning,
		Model:          req.Model,
		CWD:            req.CWD,
		TimeoutSeconds: req.TimeoutSeconds,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		Cancel:         cancel,
	}
	s.delegateMu.Lock()
	s.pruneDelegateJobsLocked(time.Now().UTC())
	s.delegateJobs[job.ID] = job
	job.NextSeq++
	job.Events = append(job.Events, delegateProgressEvent{
		Seq:  job.NextSeq,
		At:   time.Now().UTC(),
		Type: "info",
		Text: "delegate job started",
	})
	s.delegateMu.Unlock()
	log.Printf("delegate job started id=%s model=%s cwd=%s timeout_s=%d", job.ID, job.Model, job.CWD, job.TimeoutSeconds)

	go func() {
		resp, runErr := s.appServerClient.SendPromptStream(ctx, appserver.PromptRequest{
			CWD:          req.CWD,
			Prompt:       req.FullPrompt,
			Model:        req.Model,
			ThreadParams: req.Reasoning,
			TurnParams:   req.Reasoning,
			Timeout:      req.Timeout,
		}, func(ev appserver.StreamEvent) {
			switch ev.Type {
			case "thread_started":
				if strings.TrimSpace(ev.ThreadID) != "" {
					s.delegateMu.Lock()
					job.ThreadID = strings.TrimSpace(ev.ThreadID)
					job.UpdatedAt = time.Now().UTC()
					s.delegateMu.Unlock()
				}
			case "turn_started":
				if strings.TrimSpace(ev.TurnID) != "" {
					s.delegateMu.Lock()
					job.TurnID = strings.TrimSpace(ev.TurnID)
					job.UpdatedAt = time.Now().UTC()
					s.delegateMu.Unlock()
				}
			case "assistant_message":
				if strings.TrimSpace(ev.Message) != "" {
					s.delegateMu.Lock()
					job.Message = strings.TrimSpace(ev.Message)
					job.UpdatedAt = time.Now().UTC()
					s.delegateMu.Unlock()
				}
				delta := strings.TrimSpace(ev.Delta)
				if delta == "" {
					delta = strings.TrimSpace(ev.Message)
				}
				if delta != "" {
					s.addDelegateEvent(job, "assistant_message", delta)
				}
			case "item_completed":
				if strings.TrimSpace(ev.Message) != "" {
					s.addDelegateEvent(job, "item_completed", ev.Message)
				}
			case "context_usage":
				usage := fmt.Sprintf("context usage: %d/%d", ev.ContextUsed, ev.ContextMax)
				s.addDelegateEvent(job, "context_usage", usage)
			case "context_compact":
				s.addDelegateEvent(job, "context_compact", "delegate context compacted")
			case "error":
				if strings.TrimSpace(ev.Error) != "" {
					s.addDelegateEvent(job, "error", ev.Error)
				}
			}
		})
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				s.finalizeDelegateJob(job, delegateStatusCanceled, "delegate job canceled", nil)
				return
			}
			if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				s.finalizeDelegateJob(job, delegateStatusFailed, "delegate job timed out", nil)
				return
			}
			s.finalizeDelegateJob(job, delegateStatusFailed, fmt.Sprintf("app-server inference failed: %v", runErr), nil)
			return
		}
		s.finalizeDelegateJob(job, delegateStatusCompleted, "", resp)
	}()

	return map[string]interface{}{
		"ok":              true,
		"job_id":          job.ID,
		"status":          job.Status,
		"model":           job.Model,
		"timeout_seconds": job.TimeoutSeconds,
		"started_at":      job.CreatedAt.Format(time.RFC3339),
		"poll_hint":       "Call delegate_to_model_status with this job_id and after_seq cursor for incremental updates.",
	}, nil
}

func newDelegateJobID() string {
	entropy := make([]byte, 8)
	if _, err := rand.Read(entropy); err != nil {
		return fmt.Sprintf("dlg-%d", time.Now().UnixNano())
	}
	return "dlg-" + hex.EncodeToString(entropy)
}

func truncateDelegateEventText(text string, max int) string {
	s := strings.TrimSpace(text)
	if max <= 0 || len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "..."
}

func (s *Server) addDelegateEvent(job *delegateJob, typ, text string) {
	eventText := truncateDelegateEventText(text, 1800)
	if strings.TrimSpace(eventText) == "" {
		return
	}
	now := time.Now().UTC()
	s.delegateMu.Lock()
	defer s.delegateMu.Unlock()
	if job.Status == delegateStatusCompleted || job.Status == delegateStatusFailed || job.Status == delegateStatusCanceled {
		// Keep finished jobs immutable except explicit finalization paths.
		return
	}
	job.NextSeq++
	job.Events = append(job.Events, delegateProgressEvent{
		Seq:  job.NextSeq,
		At:   now,
		Type: typ,
		Text: eventText,
	})
	if len(job.Events) > delegateEventHistoryLimit {
		job.Events = append([]delegateProgressEvent(nil), job.Events[len(job.Events)-delegateEventHistoryLimit:]...)
	}
	job.UpdatedAt = now
}

func (s *Server) getDelegateJob(jobID string) (*delegateJob, error) {
	key := strings.TrimSpace(jobID)
	if key == "" {
		return nil, errors.New("job_id is required")
	}
	s.delegateMu.Lock()
	defer s.delegateMu.Unlock()
	job := s.delegateJobs[key]
	if job == nil {
		return nil, fmt.Errorf("delegate job not found: %s", key)
	}
	return job, nil
}

func (s *Server) finalizeDelegateJob(job *delegateJob, status, errText string, resp *appserver.PromptResponse) {
	now := time.Now().UTC()
	s.delegateMu.Lock()
	defer s.delegateMu.Unlock()
	if resp != nil {
		if strings.TrimSpace(resp.ThreadID) != "" {
			job.ThreadID = strings.TrimSpace(resp.ThreadID)
		}
		if strings.TrimSpace(resp.TurnID) != "" {
			job.TurnID = strings.TrimSpace(resp.TurnID)
		}
		if strings.TrimSpace(resp.Message) != "" {
			job.Message = strings.TrimSpace(resp.Message)
		}
		if len(resp.FileChanges) > 0 {
			job.FilesChanged = append([]string(nil), resp.FileChanges...)
		}
	}
	job.Status = status
	job.Error = strings.TrimSpace(errText)
	job.UpdatedAt = now
	job.FinishedAt = now
	job.Cancel = nil
	job.NextSeq++
	finalType := "info"
	finalText := "delegate job finished"
	switch status {
	case delegateStatusCompleted:
		finalText = "delegate job completed"
	case delegateStatusCanceled:
		finalType = "warning"
		finalText = "delegate job canceled"
	case delegateStatusFailed:
		finalType = "error"
		if job.Error != "" {
			finalText = job.Error
		} else {
			finalText = "delegate job failed"
		}
	}
	job.Events = append(job.Events, delegateProgressEvent{
		Seq:  job.NextSeq,
		At:   now,
		Type: finalType,
		Text: finalText,
	})
	if len(job.Events) > delegateEventHistoryLimit {
		job.Events = append([]delegateProgressEvent(nil), job.Events[len(job.Events)-delegateEventHistoryLimit:]...)
	}
	log.Printf(
		"delegate job finished id=%s status=%s model=%s cwd=%s thread=%s turn=%s files_changed=%d error=%q",
		job.ID,
		job.Status,
		job.Model,
		job.CWD,
		job.ThreadID,
		job.TurnID,
		len(job.FilesChanged),
		job.Error,
	)
}

func (s *Server) pruneDelegateJobsLocked(now time.Time) {
	for id, job := range s.delegateJobs {
		if job == nil || job.Status == delegateStatusRunning {
			continue
		}
		finishedAt := job.FinishedAt
		if finishedAt.IsZero() {
			finishedAt = job.UpdatedAt
		}
		if finishedAt.IsZero() {
			finishedAt = job.CreatedAt
		}
		if finishedAt.IsZero() || now.Sub(finishedAt) <= delegateFinishedJobRetention {
			continue
		}
		delete(s.delegateJobs, id)
	}
}

func (s *Server) delegateToModelStatus(args map[string]interface{}) (map[string]interface{}, error) {
	job, err := s.getDelegateJob(strArg(args, "job_id"))
	if err != nil {
		return nil, err
	}
	afterSeq := intArg(args, "after_seq", 0)
	if afterSeq < 0 {
		afterSeq = 0
	}
	maxEvents := intArg(args, "max_events", delegateStatusDefaultMaxEvents)
	if maxEvents <= 0 {
		maxEvents = delegateStatusDefaultMaxEvents
	}
	if maxEvents > delegateStatusHardMaxEvents {
		maxEvents = delegateStatusHardMaxEvents
	}
	s.delegateMu.Lock()
	defer s.delegateMu.Unlock()
	events := make([]map[string]interface{}, 0, maxEvents)
	nextAfterSeq := afterSeq
	for _, ev := range job.Events {
		if ev.Seq <= int64(afterSeq) {
			continue
		}
		events = append(events, map[string]interface{}{
			"seq":  ev.Seq,
			"at":   ev.At.Format(time.RFC3339),
			"type": ev.Type,
			"text": ev.Text,
		})
		nextAfterSeq = int(ev.Seq)
		if len(events) >= maxEvents {
			break
		}
	}
	latestSeq := int(job.NextSeq)
	done := job.Status != delegateStatusRunning
	return map[string]interface{}{
		"ok":              true,
		"job_id":          job.ID,
		"status":          job.Status,
		"done":            done,
		"model":           job.Model,
		"cwd":             job.CWD,
		"thread_id":       job.ThreadID,
		"turn_id":         job.TurnID,
		"message":         job.Message,
		"files_changed":   append([]string(nil), job.FilesChanged...),
		"error":           job.Error,
		"created_at":      job.CreatedAt.Format(time.RFC3339),
		"updated_at":      job.UpdatedAt.Format(time.RFC3339),
		"finished_at":     formatTimestamp(job.FinishedAt),
		"events":          events,
		"after_seq":       nextAfterSeq,
		"latest_seq":      latestSeq,
		"has_more":        latestSeq > nextAfterSeq,
		"timeout_seconds": job.TimeoutSeconds,
	}, nil
}

func formatTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func (s *Server) delegateToModelCancel(args map[string]interface{}) (map[string]interface{}, error) {
	job, err := s.getDelegateJob(strArg(args, "job_id"))
	if err != nil {
		return nil, err
	}
	s.delegateMu.Lock()
	status := job.Status
	cancel := job.Cancel
	job.UpdatedAt = time.Now().UTC()
	if status == delegateStatusRunning {
		job.NextSeq++
		job.Events = append(job.Events, delegateProgressEvent{
			Seq:  job.NextSeq,
			At:   time.Now().UTC(),
			Type: "warning",
			Text: "cancel requested",
		})
		if len(job.Events) > delegateEventHistoryLimit {
			job.Events = append([]delegateProgressEvent(nil), job.Events[len(job.Events)-delegateEventHistoryLimit:]...)
		}
	}
	s.delegateMu.Unlock()
	if status == delegateStatusRunning && cancel != nil {
		cancel()
	}
	return map[string]interface{}{
		"ok":       true,
		"job_id":   job.ID,
		"status":   status,
		"canceled": status == delegateStatusRunning && cancel != nil,
		"done":     status != delegateStatusRunning,
	}, nil
}

func normalizeScopePath(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	clean := filepath.Clean(s)
	abs, err := filepath.Abs(clean)
	if err != nil {
		return clean
	}
	return abs
}

func delegateJobInScope(jobCWD, scope string) bool {
	if strings.TrimSpace(scope) == "" {
		return true
	}
	j := normalizeScopePath(jobCWD)
	s := normalizeScopePath(scope)
	if j == "" || s == "" {
		return false
	}
	if j == s {
		return true
	}
	prefix := s + string(os.PathSeparator)
	return strings.HasPrefix(j, prefix)
}

func (s *Server) delegateToModelCancelAll(args map[string]interface{}) (map[string]interface{}, error) {
	scope := strArg(args, "cwd_prefix")
	s.delegateMu.Lock()
	now := time.Now().UTC()
	s.pruneDelegateJobsLocked(now)
	type candidate struct {
		id     string
		cancel context.CancelFunc
	}
	toCancel := make([]candidate, 0)
	for id, job := range s.delegateJobs {
		if job == nil || job.Status != delegateStatusRunning {
			continue
		}
		if !delegateJobInScope(job.CWD, scope) {
			continue
		}
		job.NextSeq++
		job.Events = append(job.Events, delegateProgressEvent{
			Seq:  job.NextSeq,
			At:   now,
			Type: "warning",
			Text: "cancel requested",
		})
		if len(job.Events) > delegateEventHistoryLimit {
			job.Events = append([]delegateProgressEvent(nil), job.Events[len(job.Events)-delegateEventHistoryLimit:]...)
		}
		job.UpdatedAt = now
		if job.Cancel != nil {
			toCancel = append(toCancel, candidate{id: id, cancel: job.Cancel})
		}
	}
	s.delegateMu.Unlock()
	for _, c := range toCancel {
		c.cancel()
	}
	jobIDs := make([]string, 0, len(toCancel))
	for _, c := range toCancel {
		jobIDs = append(jobIDs, c.id)
	}
	return map[string]interface{}{
		"ok":       true,
		"canceled": len(toCancel),
		"job_ids":  jobIDs,
	}, nil
}

func (s *Server) delegateToModelActiveCount(args map[string]interface{}) (map[string]interface{}, error) {
	scope := strArg(args, "cwd_prefix")
	s.delegateMu.Lock()
	defer s.delegateMu.Unlock()
	s.pruneDelegateJobsLocked(time.Now().UTC())
	count := 0
	jobIDs := make([]string, 0)
	for id, job := range s.delegateJobs {
		if job == nil || job.Status != delegateStatusRunning {
			continue
		}
		if !delegateJobInScope(job.CWD, scope) {
			continue
		}
		count++
		jobIDs = append(jobIDs, id)
	}
	return map[string]interface{}{
		"ok":      true,
		"active":  count,
		"job_ids": jobIDs,
	}, nil
}

func isPathWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Server) resolveTempArtifactsDir(cwdArg string) (string, string, error) {
	cwd := strings.TrimSpace(cwdArg)
	if cwd == "" {
		cwd = s.adapter.ProjectDir()
	}
	if strings.TrimSpace(cwd) == "" {
		cwd = "."
	}
	rootAbs, err := filepath.Abs(cwd)
	if err != nil {
		return "", "", err
	}
	tmpAbs := filepath.Clean(filepath.Join(rootAbs, tempArtifactsDirRel))
	if !isPathWithinDir(tmpAbs, rootAbs) {
		return "", "", errors.New("temp artifacts directory escapes project root")
	}
	return rootAbs, tmpAbs, nil
}

func (s *Server) tempFileCreate(args map[string]interface{}) (map[string]interface{}, error) {
	rootAbs, tmpAbs, err := s.resolveTempArtifactsDir(strArg(args, "cwd"))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmpAbs, 0755); err != nil {
		return nil, err
	}
	prefix := strings.TrimSpace(strArg(args, "prefix"))
	if prefix == "" {
		prefix = "canvas"
	}
	prefix = strings.ReplaceAll(prefix, string(os.PathSeparator), "-")
	prefix = strings.ReplaceAll(prefix, "/", "-")
	suffix := strings.TrimSpace(strArg(args, "suffix"))
	if suffix == "" {
		suffix = ".md"
	}
	suffix = strings.ReplaceAll(suffix, string(os.PathSeparator), "")
	suffix = strings.ReplaceAll(suffix, "/", "")
	pattern := prefix + "-*" + suffix
	f, err := os.CreateTemp(tmpAbs, pattern)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	content := strArg(args, "content")
	if content != "" {
		if _, err := f.WriteString(content); err != nil {
			return nil, err
		}
	}
	absPath := filepath.Clean(f.Name())
	relPath, err := filepath.Rel(rootAbs, absPath)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"ok":       true,
		"path":     filepath.ToSlash(relPath),
		"abs_path": absPath,
	}, nil
}

func (s *Server) tempFileRemove(args map[string]interface{}) (map[string]interface{}, error) {
	target := strings.TrimSpace(strArg(args, "path"))
	if target == "" {
		return nil, errors.New("path is required")
	}
	rootAbs, tmpAbs, err := s.resolveTempArtifactsDir(strArg(args, "cwd"))
	if err != nil {
		return nil, err
	}
	var absPath string
	if filepath.IsAbs(target) {
		absPath = filepath.Clean(target)
	} else {
		absPath = filepath.Clean(filepath.Join(rootAbs, target))
	}
	if !isPathWithinDir(absPath, tmpAbs) {
		return nil, errors.New("path must be under .tabura/artifacts/tmp")
	}
	err = os.Remove(absPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	removed := err == nil
	relPath, relErr := filepath.Rel(rootAbs, absPath)
	if relErr != nil {
		relPath = absPath
	}
	return map[string]interface{}{
		"ok":      true,
		"path":    filepath.ToSlash(relPath),
		"removed": removed,
	}, nil
}

func (s *Server) canvasImportHandoff(sessionID string, args map[string]interface{}) (map[string]interface{}, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("session_id is required")
	}
	handoffID := strArg(args, "handoff_id")
	if strings.TrimSpace(handoffID) == "" {
		return nil, errors.New("handoff_id is required")
	}
	producerMCPURL := strArg(args, "producer_mcp_url")
	if strings.TrimSpace(producerMCPURL) == "" {
		producerMCPURL = defaultProducerMCPURL
	}
	peek, err := mcpToolCall(producerMCPURL, "handoff.peek", map[string]interface{}{"handoff_id": handoffID})
	if err != nil {
		return nil, fmt.Errorf("handoff.peek failed: %w", err)
	}
	consume, err := mcpToolCall(producerMCPURL, "handoff.consume", map[string]interface{}{"handoff_id": handoffID})
	if err != nil {
		return nil, fmt.Errorf("handoff.consume failed: %w", err)
	}
	env, err := decodeEnvelope(consume)
	if err != nil {
		return nil, err
	}
	peekKind := strings.TrimSpace(fmt.Sprint(peek["kind"]))
	if peekKind != "" && peekKind != env.Kind {
		return nil, fmt.Errorf("handoff kind changed between peek and consume: %s != %s", peekKind, env.Kind)
	}
	title := strings.TrimSpace(strArg(args, "title"))
	switch env.Kind {
	case handoffKindFile:
		return s.importFile(sessionID, handoffID, title, env)
	default:
		return nil, fmt.Errorf("unsupported handoff kind: %s", env.Kind)
	}
}

func mcpToolCall(mcpURL, name string, arguments map[string]interface{}) (map[string]interface{}, error) {
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      name,
			"arguments": arguments,
		},
	}
	body, _ := json.Marshal(request)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Post(mcpURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, err
	}
	if rpcErr, ok := rpcResp["error"].(map[string]interface{}); ok {
		return nil, fmt.Errorf("%v", rpcErr["message"])
	}

	result, _ := rpcResp["result"].(map[string]interface{})
	if result == nil {
		return nil, errors.New("missing result")
	}
	if isErr, _ := result["isError"].(bool); isErr {
		if sc, ok := result["structuredContent"].(map[string]interface{}); ok {
			if msg, ok := sc["error"].(string); ok && strings.TrimSpace(msg) != "" {
				return nil, errors.New(msg)
			}
		}
		return nil, errors.New("remote tool returned error")
	}
	structured, _ := result["structuredContent"].(map[string]interface{})
	if structured == nil {
		return nil, errors.New("missing structuredContent")
	}
	return structured, nil
}

func decodeEnvelope(payload map[string]interface{}) (handoffEnvelope, error) {
	raw, _ := json.Marshal(payload)
	var env handoffEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return handoffEnvelope{}, fmt.Errorf("invalid handoff envelope: %w", err)
	}
	if strings.TrimSpace(env.Kind) == "" {
		return handoffEnvelope{}, errors.New("handoff envelope missing kind")
	}
	if env.Meta == nil {
		env.Meta = map[string]interface{}{}
	}
	if env.Payload == nil {
		env.Payload = map[string]interface{}{}
	}
	return env, nil
}

func (s *Server) importFile(sessionID, handoffID, title string, env handoffEnvelope) (map[string]interface{}, error) {
	contentB64 := strings.TrimSpace(fmt.Sprint(env.Payload["content_base64"]))
	if contentB64 == "" || contentB64 == "<nil>" {
		return nil, errors.New("file payload missing content_base64")
	}
	content, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		return nil, fmt.Errorf("invalid file payload base64: %w", err)
	}
	if err := verifyFileIntegrity(env.Meta, content); err != nil {
		return nil, err
	}

	filename := sanitizeFilename(strings.TrimSpace(fmt.Sprint(env.Meta["filename"])))
	if filename == "" || filename == "<nil>" {
		filename = "handoff-file"
	}
	mimeType := strings.TrimSpace(fmt.Sprint(env.Meta["mime_type"]))
	if mimeType == "" || mimeType == "<nil>" {
		mimeType = mime.TypeByExtension(filepath.Ext(filename))
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "application/octet-stream"
	}
	if strings.TrimSpace(title) == "" {
		title = filename
	}

	relativePath, err := s.writeImportedFile(handoffID, filename, content)
	if err != nil {
		return nil, err
	}

	var shown map[string]interface{}
	switch {
	case mimeType == "application/pdf":
		shown, err = s.adapter.CanvasArtifactShow(sessionID, "pdf", title, "", relativePath, 0, "", nil)
	case strings.HasPrefix(mimeType, "image/"):
		shown, err = s.adapter.CanvasArtifactShow(sessionID, "image", title, "", relativePath, 0, "", nil)
	case strings.HasPrefix(mimeType, "text/") && utf8.Valid(content):
		shown, err = s.adapter.CanvasArtifactShow(sessionID, "text", title, string(content), "", 0, "", nil)
	default:
		summary := fmt.Sprintf("# Imported File\n\n- Filename: `%s`\n- MIME: `%s`\n- Size: `%d` bytes\n- Stored at: `%s`\n\nPreview not available for this file type.", filename, mimeType, len(content), relativePath)
		shown, err = s.adapter.CanvasArtifactShow(sessionID, "text", title, summary, "", 0, "", nil)
	}
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"artifact_id": shown["artifact_id"],
		"title":       title,
		"handoff_id":  handoffID,
		"kind":        env.Kind,
		"mime_type":   mimeType,
		"path":        relativePath,
		"size_bytes":  len(content),
	}, nil
}

func verifyFileIntegrity(meta map[string]interface{}, content []byte) error {
	if meta == nil {
		return nil
	}
	if raw, ok := meta["size_bytes"]; ok {
		want, has := asInt(raw)
		if has && want >= 0 && len(content) != want {
			return fmt.Errorf("file size mismatch: expected %d, got %d", want, len(content))
		}
	}
	hash := strings.ToLower(strings.TrimSpace(fmt.Sprint(meta["sha256"])))
	if hash != "" && hash != "<nil>" {
		sum := sha256.Sum256(content)
		if fmt.Sprintf("%x", sum) != hash {
			return errors.New("file sha256 mismatch")
		}
	}
	return nil
}

func asInt(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func sanitizeFilename(name string) string {
	if name == "" {
		return ""
	}
	base := filepath.Base(name)
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.TrimSpace(b.String())
}

func (s *Server) writeImportedFile(handoffID, filename string, content []byte) (string, error) {
	projectDir := s.adapter.ProjectDir()
	if strings.TrimSpace(projectDir) == "" {
		return "", errors.New("project directory not configured")
	}
	importDir := filepath.Join(projectDir, ".tabura", "artifacts", "imports")
	if err := os.MkdirAll(importDir, 0o755); err != nil {
		return "", err
	}
	prefix := handoffID
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	safeName := sanitizeFilename(filename)
	if safeName == "" {
		safeName = "artifact.bin"
	}
	fullPath := filepath.Join(importDir, prefix+"-"+safeName)
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(projectDir, fullPath)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func (s *Server) dispatchResourceRead(params map[string]interface{}) (map[string]interface{}, *RPCError) {
	uri, _ := params["uri"].(string)
	if strings.TrimSpace(uri) == "" {
		return nil, &RPCError{Code: -32602, Message: "resources/read requires uri"}
	}
	content, err := readResource(s.adapter, uri)
	if err != nil {
		return nil, &RPCError{Code: -32002, Message: err.Error()}
	}
	return map[string]interface{}{"contents": []map[string]interface{}{content}}, nil
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]interface{}, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	default:
		return def
	}
}

func toolDefinitions() []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(surface.MCPTools))
	for _, tool := range surface.MCPTools {
		schema := map[string]interface{}{"type": "object"}
		if len(tool.Required) > 0 {
			schema["required"] = append([]string(nil), tool.Required...)
		}
		if len(tool.Properties) > 0 {
			props := make(map[string]interface{}, len(tool.Properties))
			for k, v := range tool.Properties {
				prop := map[string]interface{}{
					"type":        v.Type,
					"description": v.Description,
				}
				if len(v.Enum) > 0 {
					prop["enum"] = v.Enum
				}
				props[k] = prop
			}
			schema["properties"] = props
		}
		out = append(out, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": schema,
		})
	}
	return out
}

func resourceTemplates() []map[string]interface{} {
	return []map[string]interface{}{
		{"uriTemplate": "tabura://session/{session_id}", "name": "Canvas Session Status", "mimeType": "application/json", "description": "Current status for a canvas session."},
		{"uriTemplate": "tabura://session/{session_id}/history", "name": "Canvas Session History", "mimeType": "application/json", "description": "Recent event history for a canvas session."},
	}
}

func resourcesList(adapter *canvas.Adapter) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, sid := range adapter.ListSessions() {
		for _, uri := range []string{"tabura://session/" + sid, "tabura://session/" + sid + "/history"} {
			out = append(out, map[string]interface{}{"uri": uri, "name": uri, "mimeType": "application/json"})
		}
	}
	return out
}

func readResource(adapter *canvas.Adapter, uri string) (map[string]interface{}, error) {
	if !strings.HasPrefix(uri, "tabura://session/") {
		return nil, fmt.Errorf("unsupported uri: %s", uri)
	}
	path := strings.TrimPrefix(uri, "tabura://session/")
	if path == "" {
		return nil, fmt.Errorf("missing session id")
	}
	parts := strings.Split(path, "/")
	sid := parts[0]
	var payload map[string]interface{}
	if len(parts) == 1 {
		payload = adapter.CanvasStatus(sid)
	} else {
		switch parts[1] {
		case "history":
			payload = adapter.CanvasHistory(sid, 100)
		default:
			return nil, fmt.Errorf("unsupported session resource: %s", uri)
		}
	}
	b, _ := json.Marshal(payload)
	return map[string]interface{}{"uri": uri, "mimeType": "application/json", "text": string(b)}, nil
}

func RunStdio(adapter *canvas.Adapter) int {
	s := NewServer(adapter)
	reader := bufio.NewReader(os.Stdin)
	for {
		msg, framed, err := readMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0
			}
			_ = writeMessage(os.Stdout, map[string]interface{}{"jsonrpc": "2.0", "id": nil, "error": RPCError{Code: -32700, Message: err.Error()}}, framed)
			continue
		}
		resp := s.DispatchMessage(msg)
		if resp == nil {
			continue
		}
		if err := writeMessage(os.Stdout, resp, framed); err != nil {
			return 1
		}
	}
}

func readMessage(r *bufio.Reader) (map[string]interface{}, bool, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && len(line) > 0 {
			// proceed
		} else {
			return nil, true, err
		}
	}
	if len(bytes.TrimSpace(line)) == 0 {
		return nil, true, io.EOF
	}
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var payload map[string]interface{}
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			return nil, false, err
		}
		return payload, false, nil
	}

	headers := map[string]string{}
	for {
		t := strings.TrimSpace(string(line))
		if t == "" {
			break
		}
		parts := strings.SplitN(t, ":", 2)
		if len(parts) != 2 {
			return nil, true, fmt.Errorf("invalid header line")
		}
		headers[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
		next, err := r.ReadBytes('\n')
		if err != nil {
			return nil, true, err
		}
		line = next
	}
	lstr, ok := headers["content-length"]
	if !ok {
		return nil, true, fmt.Errorf("missing content-length header")
	}
	length, err := strconv.Atoi(lstr)
	if err != nil || length < 0 {
		return nil, true, fmt.Errorf("invalid content-length header")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, true, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, true, err
	}
	return payload, true, nil
}

func writeMessage(w io.Writer, payload map[string]interface{}, framed bool) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if framed {
		if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
