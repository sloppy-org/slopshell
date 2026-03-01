package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/modelprofile"
)

func resolveModelAlias(raw string) string {
	resolved := modelprofile.ResolveModel(raw, modelprofile.AliasCodex)
	if strings.TrimSpace(resolved) != "" {
		return resolved
	}
	return strings.TrimSpace(raw)
}

// delegateReasoningParams returns high reasoning effort for non-spark models
// (gpt-5.3-codex, gpt-5.2) so delegated tasks get full reasoning budget.
func delegateReasoningParams(model string) map[string]interface{} {
	return modelprofile.DelegateReasoningParams(model)
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
