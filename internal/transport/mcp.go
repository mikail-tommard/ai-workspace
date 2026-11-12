// Package transport adapts MCP, REST and WebSocket to the same workspace service.
package transport

import (
	"aiworkspace/internal/workspace"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

const MaxBody = 1 << 20
const Protocol = "2025-11-25"

func supported(v string) bool { return v == Protocol || v == "2025-06-18" || v == "2025-03-26" }

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type mcpSession struct {
	version string
	ready   bool
	expires time.Time
}
type MCP struct {
	Service  *workspace.Service
	mu       sync.Mutex
	sessions map[string]mcpSession
}

func NewMCP(s *workspace.Service) *MCP { return &MCP{Service: s, sessions: map[string]mcpSession{}} }
func parseRPC(raw []byte) (rpcRequest, *rpcResponse) {
	var r rpcRequest
	if json.Unmarshal(raw, &r) != nil {
		return r, &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "Parse error"}}
	}
	if r.JSONRPC != "2.0" || r.Method == "" || string(r.ID) == "null" {
		return r, &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32600, "Invalid Request"}}
	}
	if len(r.ID) > 0 {
		var id any
		_ = json.Unmarshal(r.ID, &id)
		switch id.(type) {
		case string, float64:
		default:
			return r, &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32600, "Invalid request ID"}}
		}
	}
	return r, nil
}
func (m *MCP) call(ctx context.Context, r rpcRequest, ready bool) *rpcResponse {
	if len(r.ID) == 0 {
		return nil
	}
	res := &rpcResponse{JSONRPC: "2.0", ID: r.ID}
	fail := func(code int, msg string) *rpcResponse { res.Error = &rpcError{code, msg}; return res }
	switch r.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string         `json:"protocolVersion"`
			ClientInfo      map[string]any `json:"clientInfo"`
			Capabilities    map[string]any `json:"capabilities"`
		}
		if json.Unmarshal(r.Params, &p) != nil || p.ProtocolVersion == "" || p.ClientInfo == nil || p.Capabilities == nil {
			return fail(-32602, "Invalid initialize parameters")
		}
		version := p.ProtocolVersion
		if !supported(version) {
			version = Protocol
		}
		res.Result = map[string]any{"protocolVersion": version, "serverInfo": map[string]string{"name": "ai-workspace", "version": "1.0.0"}, "capabilities": map[string]any{"tools": map[string]bool{"listChanged": false}}, "instructions": "Manage a local visual workspace. Default workspace_id is default. Math and quiz answers are checked deterministically. Use math_create_problem for math cards; widget_create type=table for tables."}
	case "ping":
		res.Result = map[string]any{}
	case "tools/list":
		if !ready {
			return fail(-32000, "Initialize session first")
		}
		res.Result = map[string]any{"tools": m.Service.Tools()}
	case "tools/call":
		if !ready {
			return fail(-32000, "Initialize session first")
		}
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Meta      json.RawMessage `json:"_meta,omitempty"`
		}
		if json.Unmarshal(r.Params, &p) != nil || p.Name == "" {
			return fail(-32602, "Invalid tools/call parameters")
		}
		known := false
		for _, t := range m.Service.Tools() {
			if t.Name == p.Name {
				known = true
				break
			}
		}
		if !known {
			return fail(-32602, "Unknown tool: "+p.Name)
		}
		if len(p.Arguments) == 0 {
			p.Arguments = json.RawMessage("{}")
		}
		result, err := m.Service.Execute(ctx, p.Name, p.Arguments)
		isError := err != nil
		if err != nil {
			result = workspace.Result{"error": err.Error()}
		}
		b, _ := json.Marshal(result)
		res.Result = map[string]any{"content": []map[string]string{{"type": "text", "text": string(b)}}, "structuredContent": result, "isError": isError}
	default:
		return fail(-32601, "Method not found")
	}
	return res
}

// Stdio uses newline-delimited JSON-RPC. Logs belong exclusively on stderr.
func (m *MCP) Stdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), MaxBody)
	encoder := json.NewEncoder(out)
	initialized, ready := false, false
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		r, res := parseRPC(raw)
		if res == nil {
			if r.Method == "notifications/initialized" && initialized {
				ready = true
			}
			res = m.call(ctx, r, ready)
			if r.Method == "initialize" && res != nil && res.Error == nil {
				initialized = true
			}
		}
		if res != nil {
			if e := encoder.Encode(res); e != nil {
				return e
			}
		}
	}
	return scanner.Err()
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (m *MCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" && r.Method != "GET" && r.Method != "DELETE" {
		w.Header().Set("Allow", "POST, GET, DELETE")
		http.Error(w, "method not allowed", 405)
		return
	}
	now := time.Now()
	m.mu.Lock()
	for id, s := range m.sessions {
		if s.expires.Before(now) {
			delete(m.sessions, id)
		}
	}
	sessionID := r.Header.Get("Mcp-Session-Id")
	session, ok := m.sessions[sessionID]
	if ok {
		session.expires = now.Add(time.Hour)
		m.sessions[sessionID] = session
	}
	m.mu.Unlock()
	if sessionID != "" && !ok {
		http.Error(w, "unknown or expired MCP session", 404)
		return
	}
	version := r.Header.Get("MCP-Protocol-Version")
	if version != "" && !supported(version) {
		http.Error(w, "unsupported MCP protocol version", 400)
		return
	}
	if ok && version != "" && version != session.version {
		http.Error(w, "protocol version does not match session", 400)
		return
	}
	if r.Method == "GET" {
		if !ok {
			http.Error(w, "session required", 400)
			return
		}
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "SSE not offered; JSON responses are supported", 405)
		return
	}
	if r.Method == "DELETE" {
		if !ok {
			http.Error(w, "session required", 400)
			return
		}
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		w.WriteHeader(204)
		return
	}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		http.Error(w, "application/json required", 415)
		return
	}
	accept := r.Header.Get("Accept")
	if !strings.Contains(accept, "application/json") || !strings.Contains(accept, "text/event-stream") {
		http.Error(w, "Accept must include application/json and text/event-stream", 406)
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if e != nil {
		http.Error(w, "request too large", 413)
		return
	}
	req, res := parseRPC(body)
	if res != nil {
		writeJSON(w, 400, res)
		return
	}
	if req.Method != "initialize" && !ok {
		http.Error(w, "Mcp-Session-Id required", 400)
		return
	}
	if req.Method == "initialize" && ok {
		http.Error(w, "session already initialized", 400)
		return
	}
	if len(req.ID) == 0 {
		if req.Method == "notifications/initialized" {
			session.ready = true
			m.mu.Lock()
			m.sessions[sessionID] = session
			m.mu.Unlock()
		}
		w.WriteHeader(202)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res = m.call(ctx, req, session.ready)
	if req.Method == "initialize" && res != nil && res.Error == nil {
		m.mu.Lock()
		if len(m.sessions) >= 256 {
			m.mu.Unlock()
			http.Error(w, "session limit", 503)
			return
		}
		id := workspace.ID()
		version := res.Result.(map[string]any)["protocolVersion"].(string)
		m.sessions[id] = mcpSession{version: version, expires: now.Add(time.Hour)}
		m.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", id)
	}
	if res == nil {
		http.Error(w, fmt.Sprint("request ID required"), 400)
		return
	}
	writeJSON(w, 200, res)
}
