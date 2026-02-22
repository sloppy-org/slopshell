package mcp

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/krystophny/tabula/internal/canvas"
	"github.com/krystophny/tabula/internal/surface"
)

const (
	ServerName            = "tabula"
	ServerVersion         = "0.0.6-dev"
	LatestProtocolVersion = "2025-03-26"
	defaultProducerMCPURL = "http://127.0.0.1:8090/mcp"
	handoffKindFile       = "file"
	handoffKindMailHeader = "mail_headers"
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
	adapter *canvas.Adapter
}

type handoffEnvelope struct {
	SpecVersion string                 `json:"spec_version"`
	HandoffID   string                 `json:"handoff_id"`
	Kind        string                 `json:"kind"`
	CreatedAt   string                 `json:"created_at"`
	Meta        map[string]interface{} `json:"meta"`
	Payload     map[string]interface{} `json:"payload"`
}

func NewServer(adapter *canvas.Adapter) *Server {
	return &Server{adapter: adapter}
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
		return s.adapter.CanvasArtifactShow(
			sid,
			strArg(args, "kind"),
			strArg(args, "title"),
			strArg(args, "markdown_or_text"),
			strArg(args, "path"),
			intArg(args, "page", 0),
			strArg(args, "reason"),
			nil,
		)
	case "canvas_render_text":
		return s.adapter.CanvasArtifactShow(sid, "text", strArg(args, "title"), strArg(args, "markdown_or_text"), "", 0, "", nil)
	case "canvas_render_image":
		return s.adapter.CanvasArtifactShow(sid, "image", strArg(args, "title"), "", strArg(args, "path"), 0, "", nil)
	case "canvas_render_pdf":
		return s.adapter.CanvasArtifactShow(sid, "pdf", strArg(args, "title"), "", strArg(args, "path"), intArg(args, "page", 0), "", nil)
	case "canvas_clear":
		return s.adapter.CanvasArtifactShow(sid, "clear", "", "", "", 0, strArg(args, "reason"), nil)
	case "canvas_mark_set":
		target, _ := args["target"].(map[string]interface{})
		return s.adapter.CanvasMarkSet(
			sid,
			strArg(args, "mark_id"),
			strArg(args, "artifact_id"),
			canvas.MarkIntent(strArg(args, "intent")),
			canvas.MarkType(strArg(args, "type")),
			canvas.TargetKind(strArg(args, "target_kind")),
			target,
			strArg(args, "comment"),
			strArg(args, "author"),
		)
	case "canvas_mark_delete":
		return s.adapter.CanvasMarkDelete(sid, strArg(args, "mark_id"))
	case "canvas_marks_list":
		return s.adapter.CanvasMarksList(sid, strArg(args, "artifact_id"), canvas.MarkIntent(strArg(args, "intent")), intArg(args, "limit", 0)), nil
	case "canvas_mark_focus":
		return s.adapter.CanvasMarkFocus(sid, strArg(args, "mark_id"))
	case "canvas_commit":
		return s.adapter.CanvasCommit(sid, strArg(args, "artifact_id"), boolArg(args, "include_draft", true))
	case "canvas_status":
		return s.adapter.CanvasStatus(sid), nil
	case "canvas_history":
		return s.adapter.CanvasHistory(sid, intArg(args, "limit", 20)), nil
	case "canvas_selection":
		return s.adapter.CanvasSelection(sid), nil
	case "canvas_import_handoff":
		return s.canvasImportHandoff(sid, args)
	default:
		return nil, errors.New("unknown tool: " + name)
	}
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
	case handoffKindMailHeader:
		return s.importMailHeaders(sessionID, handoffID, producerMCPURL, title, env)
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

type importedMailHeader struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Sender  string `json:"sender"`
	Subject string `json:"subject"`
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

func (s *Server) importMailHeaders(sessionID, handoffID, producerMCPURL, title string, env handoffEnvelope) (map[string]interface{}, error) {
	raw, _ := json.Marshal(env.Payload["headers"])
	headers := []importedMailHeader{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &headers); err != nil {
			return nil, fmt.Errorf("invalid mail_headers payload: %w", err)
		}
	}
	if strings.TrimSpace(title) == "" {
		title = "Mail Headers"
	}
	markdown := renderMailHeadersMarkdown(env.Meta, headers)
	shown, err := s.adapter.CanvasArtifactShow(sessionID, "text", title, markdown, "", 0, "", map[string]interface{}{
		"handoff_kind":      handoffKindMailHeader,
		"handoff_id":        handoffID,
		"producer_mcp_url":  producerMCPURL,
		"message_triage_v1": buildMailHeadersViewModel(env.Meta, headers),
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"artifact_id":    shown["artifact_id"],
		"title":          title,
		"handoff_id":     handoffID,
		"kind":           env.Kind,
		"imported_count": len(headers),
	}, nil
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
	importDir := filepath.Join(projectDir, ".tabula", "artifacts", "imports")
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

func renderMailHeadersMarkdown(meta map[string]interface{}, headers []importedMailHeader) string {
	var b strings.Builder
	b.WriteString("# Mail Headers\n\n")
	count := len(headers)
	if meta != nil {
		if provider := strings.TrimSpace(fmt.Sprint(meta["provider"])); provider != "" && provider != "<nil>" {
			b.WriteString("- Provider: `")
			b.WriteString(provider)
			b.WriteString("`\n")
		}
		if folder := strings.TrimSpace(fmt.Sprint(meta["folder"])); folder != "" && folder != "<nil>" {
			b.WriteString("- Folder: `")
			b.WriteString(folder)
			b.WriteString("`\n")
		}
	}
	b.WriteString("- Count: `")
	b.WriteString(strconv.Itoa(count))
	b.WriteString("`\n\n")

	for i, h := range headers {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". `")
		if strings.TrimSpace(h.Date) != "" {
			b.WriteString(h.Date)
		} else {
			b.WriteString("-")
		}
		b.WriteString("` | ")
		if strings.TrimSpace(h.Sender) != "" {
			b.WriteString(h.Sender)
		} else {
			b.WriteString("(no sender)")
		}
		b.WriteString(" | ")
		if strings.TrimSpace(h.Subject) != "" {
			b.WriteString(h.Subject)
		} else {
			b.WriteString("(no subject)")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func buildMailHeadersViewModel(meta map[string]interface{}, headers []importedMailHeader) map[string]interface{} {
	out := map[string]interface{}{
		"count":   len(headers),
		"headers": headers,
	}
	if meta == nil {
		return out
	}
	if provider := strings.TrimSpace(fmt.Sprint(meta["provider"])); provider != "" && provider != "<nil>" {
		out["provider"] = provider
	}
	if folder := strings.TrimSpace(fmt.Sprint(meta["folder"])); folder != "" && folder != "<nil>" {
		out["folder"] = folder
	}
	if count, ok := asInt(meta["count"]); ok && count >= 0 {
		out["count"] = count
	}
	return out
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

func boolArg(args map[string]interface{}, key string, def bool) bool {
	v, ok := args[key].(bool)
	if !ok {
		return def
	}
	return v
}

func toolDefinitions() []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(surface.MCPTools))
	for _, tool := range surface.MCPTools {
		schema := map[string]interface{}{"type": "object"}
		if len(tool.Required) > 0 {
			schema["required"] = append([]string(nil), tool.Required...)
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
		{"uriTemplate": "tabula://session/{session_id}", "name": "Canvas Session Status", "mimeType": "application/json", "description": "Current status for a canvas session."},
		{"uriTemplate": "tabula://session/{session_id}/marks", "name": "Canvas Session Marks", "mimeType": "application/json", "description": "Current marks for a canvas session."},
		{"uriTemplate": "tabula://session/{session_id}/history", "name": "Canvas Session History", "mimeType": "application/json", "description": "Recent event history for a canvas session."},
	}
}

func resourcesList(adapter *canvas.Adapter) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, sid := range adapter.ListSessions() {
		for _, uri := range []string{"tabula://session/" + sid, "tabula://session/" + sid + "/marks", "tabula://session/" + sid + "/history"} {
			out = append(out, map[string]interface{}{"uri": uri, "name": uri, "mimeType": "application/json"})
		}
	}
	return out
}

func readResource(adapter *canvas.Adapter, uri string) (map[string]interface{}, error) {
	if !strings.HasPrefix(uri, "tabula://session/") {
		return nil, fmt.Errorf("unsupported uri: %s", uri)
	}
	path := strings.TrimPrefix(uri, "tabula://session/")
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
		case "marks":
			payload = adapter.CanvasMarksList(sid, "", "", 0)
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
