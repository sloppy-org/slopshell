package voxtypemcp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/krystophny/tabura/internal/stt"
)

const (
	maxSessionAudioBytes   = 10 * 1024 * 1024
	defaultRequestID       = 1
	defaultSessionMimeType = "audio/webm"
)

type sessionState struct {
	MimeType  string
	StartedAt time.Time
	LastSeq   int
	Bytes     []byte
}

type Server struct {
	bind string
	port int

	mu       sync.Mutex
	sessions map[string]*sessionState
}

func NewServer(bind string, port int) *Server {
	return &Server{
		bind:     strings.TrimSpace(bind),
		port:     port,
		sessions: map[string]*sessionState{},
	}
}

func (s *Server) Start() error {
	if s.bind == "" {
		s.bind = "127.0.0.1"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/mcp", s.handleMCP)
	addr := netJoinHostPort(s.bind, s.port)
	fmt.Printf("voxtype MCP server listening on http://%s/mcp\n", addr)
	return (&http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}).ListenAndServe()
}

func netJoinHostPort(host string, port int) string {
	return host + ":" + strconv.Itoa(port)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]interface{}{
		"status": "ok",
	})
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, reqID(req), -32700, "parse error: invalid JSON")
		return
	}
	id := reqID(req)
	method := strings.TrimSpace(fmt.Sprint(req["method"]))
	params, _ := req["params"].(map[string]interface{})
	switch method {
	case "initialize":
		writeRPCResult(w, id, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]interface{}{
				"name":    "tabura-voxtype-mcp",
				"version": "0.0.6-dev",
			},
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
		})
		return
	case "tools/list":
		tools := []map[string]interface{}{
			{"name": "push_to_prompt_start", "description": "start a push-to-prompt capture session"},
			{"name": "push_to_prompt_append", "description": "append captured audio chunk to a session"},
			{"name": "push_to_prompt_stop", "description": "stop a session and transcribe the buffered audio"},
			{"name": "push_to_prompt_cancel", "description": "cancel and discard a capture session"},
			{"name": "push_to_prompt_health", "description": "health and dependency status"},
		}
		writeRPCResult(w, id, map[string]interface{}{"tools": tools})
		return
	case "tools/call":
		name := strings.TrimSpace(fmt.Sprint(params["name"]))
		args, _ := params["arguments"].(map[string]interface{})
		result, err := s.callTool(name, args)
		if err != nil {
			writeRPCError(w, id, -32000, err.Error())
			return
		}
		writeRPCResult(w, id, result)
		return
	default:
		writeRPCError(w, id, -32601, "method not found")
		return
	}
}

func reqID(req map[string]interface{}) interface{} {
	if req == nil {
		return defaultRequestID
	}
	if id, ok := req["id"]; ok {
		return id
	}
	return defaultRequestID
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeRPCResult(w http.ResponseWriter, id interface{}, structuredContent map[string]interface{}) {
	if id == nil {
		id = defaultRequestID
	}
	writeJSON(w, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"structuredContent": structuredContent,
		},
	})
}

func writeRPCError(w http.ResponseWriter, id interface{}, code int, msg string) {
	if id == nil {
		id = defaultRequestID
	}
	writeJSON(w, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": msg,
		},
	})
}

func (s *Server) callTool(name string, args map[string]interface{}) (map[string]interface{}, error) {
	switch name {
	case "push_to_prompt_start":
		return s.toolStart(args)
	case "push_to_prompt_append":
		return s.toolAppend(args)
	case "push_to_prompt_stop":
		return s.toolStop(args)
	case "push_to_prompt_cancel":
		return s.toolCancel(args)
	case "push_to_prompt_health":
		return s.toolHealth()
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func strArg(args map[string]interface{}, key string) string {
	return strings.TrimSpace(fmt.Sprint(args[key]))
}

func intArg(args map[string]interface{}, key string, def int) int {
	raw, ok := args[key]
	if !ok {
		return def
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n
		}
	}
	return def
}

func (s *Server) toolStart(args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	if sid == "" {
		return nil, errors.New("session_id is required")
	}
	mimeType := strings.ToLower(strings.TrimSpace(strArg(args, "mime_type")))
	if mimeType == "" {
		mimeType = defaultSessionMimeType
	}

	state := &sessionState{
		MimeType:  mimeType,
		StartedAt: time.Now().UTC(),
		LastSeq:   -1,
		Bytes:     make([]byte, 0, 4096),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[sid]; exists {
		return nil, fmt.Errorf("session %q already exists", sid)
	}
	s.sessions[sid] = state

	return map[string]interface{}{
		"ok":         true,
		"session_id": sid,
		"started_at": state.StartedAt.Format(time.RFC3339Nano),
	}, nil
}

func (s *Server) toolAppend(args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	if sid == "" {
		return nil, errors.New("session_id is required")
	}
	seq := intArg(args, "seq", -1)
	if seq < 0 {
		return nil, errors.New("seq must be >= 0")
	}
	encoded := strArg(args, "audio_chunk_base64")
	if encoded == "" {
		return nil, errors.New("audio_chunk_base64 is required")
	}
	chunk, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("audio_chunk_base64 must be valid base64")
	}
	if len(chunk) == 0 {
		return nil, errors.New("audio chunk is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[sid]
	if state == nil {
		return nil, fmt.Errorf("session %q not found", sid)
	}
	if state.LastSeq >= 0 && seq <= state.LastSeq {
		return nil, fmt.Errorf("seq must be strictly increasing (last=%d got=%d)", state.LastSeq, seq)
	}
	state.LastSeq = seq

	if len(state.Bytes)+len(chunk) > maxSessionAudioBytes {
		return nil, errors.New("audio payload exceeds max size")
	}
	state.Bytes = append(state.Bytes, chunk...)
	return map[string]interface{}{
		"ok":             true,
		"session_id":     sid,
		"received_seq":   seq,
		"buffered_bytes": len(state.Bytes),
	}, nil
}

func (s *Server) toolStop(args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	if sid == "" {
		return nil, errors.New("session_id is required")
	}
	s.mu.Lock()
	state := s.sessions[sid]
	if state != nil {
		delete(s.sessions, sid)
	}
	s.mu.Unlock()
	if state == nil {
		return nil, fmt.Errorf("session %q not found", sid)
	}

	if len(state.Bytes) == 0 {
		return nil, errors.New("no buffered audio for session")
	}
	start := time.Now()
	text, err := stt.TranscribeWithVoxType(state.MimeType, state.Bytes)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("voxtype returned empty transcript")
	}
	return map[string]interface{}{
		"ok":                   true,
		"session_id":           sid,
		"text":                 text,
		"language":             "",
		"language_probability": 0.0,
		"source":               "voxtype_mcp",
		"duration_ms":          time.Since(start).Milliseconds(),
	}, nil
}

func (s *Server) toolCancel(args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	if sid == "" {
		return nil, errors.New("session_id is required")
	}
	s.mu.Lock()
	_, existed := s.sessions[sid]
	delete(s.sessions, sid)
	s.mu.Unlock()

	return map[string]interface{}{
		"ok":         true,
		"session_id": sid,
		"canceled":   existed,
	}, nil
}

func (s *Server) toolHealth() (map[string]interface{}, error) {
	ffmpegPath, ffmpegErr := exec.LookPath("ffmpeg")
	voxtypePath, voxtypeErr := exec.LookPath("voxtype")

	return map[string]interface{}{
		"ok": ffmpegErr == nil && voxtypeErr == nil,
		"dependencies": map[string]interface{}{
			"ffmpeg": map[string]interface{}{
				"ok":   ffmpegErr == nil,
				"path": ffmpegPath,
			},
			"voxtype": map[string]interface{}{
				"ok":   voxtypeErr == nil,
				"path": voxtypePath,
			},
		},
	}, nil
}

