package transport

import (
	"aiworkspace/internal/assistant"
	"aiworkspace/internal/workspace"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

const testToken = "test-token-012345678901234567890123456789"

func testApp(t *testing.T, providers ...assistant.Provider) (*App, *testServer) {
	t.Helper()
	db, e := workspace.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	s, e := workspace.New(context.Background(), db)
	if e != nil {
		t.Fatal(e)
	}
	a := NewApp(s, testToken, "127.0.0.1", "0", fstest.MapFS{"index.html": {Data: []byte("<html>workspace</html>")}, "assets/app.js": {Data: []byte("console.log('ok')")}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var provider assistant.Provider = assistant.NewRealtimeClient("", "")
	if len(providers) > 0 {
		provider = providers[0]
	}
	a.Assistant = assistant.New(s, db, provider)
	ts := newMemoryServer(a.Handler())
	a.port = "8080"
	t.Cleanup(func() { a.Close(); ts.Close(); s.Close(); _ = db.Close() })
	return a, ts
}
func request(t *testing.T, ts *testServer, method, path, body string, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	r, e := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	res, e := ts.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	if e != nil {
		t.Fatal(e)
	}
	return res.StatusCode, res.Header, b
}
func auth() map[string]string {
	return map[string]string{"Authorization": "Bearer " + testToken, "Content-Type": "application/json", "Accept": "application/json, text/event-stream", "X-Workspace-Request": "1"}
}
func TestRESTAndSecurity(t *testing.T) {
	_, ts := testApp(t)
	status, _, _ := request(t, ts, "GET", "/api/workspaces/default", "", nil)
	if status != 401 {
		t.Fatal(status)
	}
	status, h, _ := request(t, ts, "GET", "/api/session", "", nil)
	if status != 200 || !strings.Contains(h.Get("Set-Cookie"), "HttpOnly") || !strings.Contains(h.Get("Set-Cookie"), "SameSite=Strict") {
		t.Fatal(status, h)
	}
	headers := auth()
	status, _, body := request(t, ts, "POST", "/api/commands", `{"tool":"math_create_problem","arguments":{"kind":"linear"}}`, headers)
	if status != 200 || !bytes.Contains(body, []byte(`"math_problem"`)) || bytes.Contains(body, []byte(`"expected"`)) {
		t.Fatal(status, string(body))
	}
	for _, path := range []string{"/api/workspaces/default", "/api/workspaces/default/events", "/api/tools", "/api/registry"} {
		status, _, _ = request(t, ts, "GET", path, "", headers)
		if status != 200 {
			t.Fatal(path, status)
		}
	}
	headers["Origin"] = "https://evil.example"
	for _, path := range []string{"/api/session", "/api/commands", "/mcp", "/ws"} {
		status, _, _ = request(t, ts, "POST", path, "{}", headers)
		if status != 403 {
			t.Fatal(path, status)
		}
	}
	delete(headers, "Origin")
	status, _, _ = request(t, ts, "POST", "/api/commands", `{"tool":"math_create_problem","arguments":{},"extra":true}`, headers)
	if status != 400 {
		t.Fatal(status)
	}
	status, _, _ = request(t, ts, "POST", "/api/commands", `{"tool":"math_create_problem","arguments":{}} {}`, headers)
	if status != 400 {
		t.Fatal(status)
	}
	status, _, _ = request(t, ts, "POST", "/api/commands", `{"tool":"math_create_problem","arguments":{"unknown":1}}`, headers)
	if status != 400 {
		t.Fatal(status)
	}
	delete(headers, "X-Workspace-Request")
	status, _, _ = request(t, ts, "POST", "/api/commands", `{}`, headers)
	if status != 403 {
		t.Fatal(status)
	}
	status, _, _ = request(t, ts, "GET", "/nested/spa/route", "", nil)
	if status != 200 {
		t.Fatal(status)
	}
	status, _, _ = request(t, ts, "GET", "/assets/missing.js", "", nil)
	if status != 404 {
		t.Fatal(status)
	}
	status, _, _ = request(t, ts, "POST", "/mcp", "{}", map[string]string{"Cookie": strings.Split(h.Get("Set-Cookie"), ";")[0]})
	if status != 401 {
		t.Fatal("MCP accepted browser session", status)
	}
	r := httptest.NewRequest("GET", "http://evil.example/api/session", nil)
	w := httptest.NewRecorder()
	app, _ := testApp(t)
	app.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("DNS rebinding accepted")
	}
}
func TestMCPHTTP(t *testing.T) {
	_, ts := testApp(t)
	headers := auth()
	status, h, b := request(t, ts, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, headers)
	if status != 200 || !bytes.Contains(b, []byte(`"protocolVersion":"2025-11-25"`)) {
		t.Fatal(status, string(b))
	}
	session := h.Get("Mcp-Session-Id")
	if session == "" {
		t.Fatal("session missing")
	}
	headers["Mcp-Session-Id"] = session
	headers["MCP-Protocol-Version"] = Protocol
	status, _, _ = request(t, ts, "POST", "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, headers)
	if status != 202 {
		t.Fatal(status)
	}
	status, _, b = request(t, ts, "POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, headers)
	var listed struct {
		Result struct {
			Tools []workspace.Tool `json:"tools"`
		} `json:"result"`
	}
	if json.Unmarshal(b, &listed) != nil || status != 200 || len(listed.Result.Tools) != 28 {
		t.Fatal(status, len(listed.Result.Tools), string(b))
	}
	status, _, b = request(t, ts, "POST", "/mcp", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"math_create_problem","arguments":{"kind":"fraction"}}}`, headers)
	if status != 200 || !bytes.Contains(b, []byte(`"isError":false`)) || bytes.Contains(b, []byte(`"expected"`)) {
		t.Fatal(status, string(b))
	}
	_, _, b = request(t, ts, "POST", "/mcp", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"math_create_problem","arguments":{"kind":"invalid"}}}`, headers)
	if !bytes.Contains(b, []byte(`"isError":true`)) {
		t.Fatal(string(b))
	}
	for _, method := range []string{"GET", "DELETE"} {
		status, _, _ = request(t, ts, method, "/mcp", "", headers)
		if method == "GET" && status != 405 || method == "DELETE" && status != 204 {
			t.Fatal(method, status)
		}
	}
	status, _, _ = request(t, ts, "POST", "/mcp", `{}`, headers)
	if status != 404 {
		t.Fatal(status)
	}
}
func TestMCPStdio(t *testing.T) {
	a, _ := testApp(t)
	input := `{"jsonrpc":"2.0","id":"a","method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"math_create_problem","arguments":{}}}
{"jsonrpc":"2.0","id":3,"method":"unknown"}
not json
`
	var output bytes.Buffer
	if e := a.MCP.Stdio(context.Background(), strings.NewReader(input), &output); e != nil {
		t.Fatal(e)
	}
	scanner := bufio.NewScanner(&output)
	n := 0
	for scanner.Scan() {
		var r rpcResponse
		if e := json.Unmarshal(scanner.Bytes(), &r); e != nil {
			t.Fatal("non JSON stdout", e)
		}
		n++
	}
	if n != 4 {
		t.Fatal(n)
	}
}
func dialWS(t *testing.T, ts *testServer, workspaceID string) (net.Conn, *bufio.Reader) {
	t.Helper()
	addr := strings.TrimPrefix(ts.URL, "http://")
	conn, e := ts.listener.Dial(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "GET /ws?workspace_id=%s HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nAuthorization: Bearer %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", workspaceID, addr, ts.URL, testToken)
	r := bufio.NewReader(conn)
	res, e := http.ReadResponse(r, &http.Request{Method: "GET"})
	if e != nil || res.StatusCode != 101 {
		t.Fatal("handshake", res, e)
	}
	return conn, r
}
func readWSEvent(t *testing.T, r *bufio.Reader) workspace.Event {
	t.Helper()
	h := make([]byte, 2)
	if _, e := io.ReadFull(r, h); e != nil {
		t.Fatal(e)
	}
	n := int(h[1] & 127)
	if n == 126 {
		b := make([]byte, 2)
		io.ReadFull(r, b)
		n = int(binary.BigEndian.Uint16(b))
	}
	if n == 127 {
		b := make([]byte, 8)
		io.ReadFull(r, b)
		n = int(binary.BigEndian.Uint64(b))
	}
	b := make([]byte, n)
	if _, e := io.ReadFull(r, b); e != nil {
		t.Fatal(e)
	}
	var ev workspace.Event
	if e := json.Unmarshal(b, &ev); e != nil {
		t.Fatal(e)
	}
	return ev
}
func TestWebSocketSyncReconnectAndMultipleClients(t *testing.T) {
	a, ts := testApp(t)
	_, r1 := dialWS(t, ts, "default")
	_, r2 := dialWS(t, ts, "default")
	s1 := readWSEvent(t, r1)
	s2 := readWSEvent(t, r2)
	if s1.Type != "state.snapshot" || s1.Version != s2.Version {
		t.Fatal("snapshot mismatch")
	}
	if _, e := a.Service.Execute(context.Background(), "math_create_problem", []byte(`{}`)); e != nil {
		t.Fatal(e)
	}
	e1, e2 := readWSEvent(t, r1), readWSEvent(t, r2)
	if e1.Type != "widget.created" || e1.Version != s1.Version+1 || e1.Version != e2.Version {
		t.Fatal("broadcast mismatch")
	}
	_, r3 := dialWS(t, ts, "default")
	reconnected := readWSEvent(t, r3)
	if reconnected.Version != e1.Version {
		t.Fatal("reconnect lost state")
	}
}
func TestWebSocketCommands(t *testing.T) {
	_, ts := testApp(t)
	conn, r := dialWS(t, ts, "default")
	readWSEvent(t, r)
	raw := []byte(`{"id":"1","tool":"math_create_problem","arguments":{"kind":"linear"}}`)
	mask := []byte{1, 2, 3, 4}
	frame := append([]byte{0x81, 0x80 | byte(len(raw))}, mask...)
	for i, v := range raw {
		frame = append(frame, v^mask[i%4])
	}
	if _, e := conn.Write(frame); e != nil {
		t.Fatal(e)
	}
	one, two := readWSEvent(t, r), readWSEvent(t, r)
	if one.Type != "widget.created" && two.Type != "widget.created" {
		t.Fatal(one.Type, two.Type)
	}
}
func TestMCPInvalidRequests(t *testing.T) {
	_, ts := testApp(t)
	for _, tc := range []struct {
		body    string
		headers map[string]string
		want    int
	}{{`{`, auth(), 400}, {`[]`, auth(), 400}, {`{}`, map[string]string{"Authorization": "Bearer " + testToken, "Content-Type": "text/plain"}, 415}, {`{}`, map[string]string{"Authorization": "Bearer " + testToken, "Content-Type": "application/json"}, 406}, {`{}`, map[string]string{"Authorization": "Bearer " + testToken, "MCP-Protocol-Version": "invalid"}, 400}} {
		status, _, _ := request(t, ts, "POST", "/mcp", tc.body, tc.headers)
		if status != tc.want {
			t.Fatal(status, tc.want)
		}
	}
}

// Tests use real HTTP/1.1 and WebSocket bytes over net.Pipe, because this build
// environment disallows listen(2). No networking behavior is mocked.
type memoryListener struct {
	pending chan net.Conn
	done    chan struct{}
	once    sync.Once
}

func newMemoryListener() *memoryListener {
	return &memoryListener{pending: make(chan net.Conn), done: make(chan struct{})}
}
func (l *memoryListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.pending:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *memoryListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *memoryListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080} }
func (l *memoryListener) Dial(ctx context.Context) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.pending <- server:
		return client, nil
	case <-l.done:
		client.Close()
		server.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		client.Close()
		server.Close()
		return nil, ctx.Err()
	}
}

type testServer struct {
	URL       string
	listener  *memoryListener
	server    *http.Server
	client    *http.Client
	transport *http.Transport
	done      chan struct{}
}

func newMemoryServer(handler http.Handler) *testServer {
	l := newMemoryListener()
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return l.Dial(ctx) }}
	s := &testServer{URL: "http://127.0.0.1:8080", listener: l, server: &http.Server{Handler: handler}, client: &http.Client{Transport: tr, Timeout: 5 * time.Second}, transport: tr, done: make(chan struct{})}
	go func() { defer close(s.done); _ = s.server.Serve(l) }()
	return s
}
func (s *testServer) Client() *http.Client { return s.client }
func (s *testServer) Close()               { s.transport.CloseIdleConnections(); _ = s.server.Close(); <-s.done }

func TestRequestSizeLimits(t *testing.T) {
	_, ts := testApp(t)
	body := strings.Repeat(" ", MaxBody+1) + "{}"
	for _, path := range []string{"/mcp", "/api/commands"} {
		status, _, _ := request(t, ts, "POST", path, body, auth())
		if status != 413 {
			t.Fatalf("%s accepted oversized request: %d", path, status)
		}
	}
}
