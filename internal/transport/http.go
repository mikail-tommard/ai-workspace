package transport

import (
	"aiworkspace/internal/assistant"
	"aiworkspace/internal/codexbridge"
	"aiworkspace/internal/desktop"
	"aiworkspace/internal/workspace"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

type App struct {
	Desktop   *desktop.Manager
	Codex     *codexbridge.Manager
	Assistant *assistant.Manager
	Service   *workspace.Service
	MCP       *MCP
	Token     string
	session   string
	host      string
	port      string
	assets    fs.FS
	logger    *slog.Logger
	wsMu      sync.Mutex
	conns     map[*wsConn]bool
}

func NewApp(s *workspace.Service, token, host, port string, assets fs.FS, logger *slog.Logger) *App {
	return &App{Service: s, MCP: NewMCP(s), Token: token, session: workspace.ID() + workspace.ID(), host: host, port: port, assets: assets, logger: logger, conns: map[*wsConn]bool{}}
}
func (a *App) Close() {
	if a.Codex != nil {
		a.Codex.Close()
	}
	if a.Desktop != nil {
		a.Desktop.Close()
	}
	if a.Assistant != nil {
		a.Assistant.Close()
	}
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	for c := range a.conns {
		_ = c.conn.Close()
	}
}
func (a *App) allowedHost(host string) bool {
	return host == "127.0.0.1:"+a.port || host == "localhost:"+a.port || host == a.host+":"+a.port
}
func (a *App) originOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, e := url.Parse(origin)
	return e == nil && u.Scheme == "http" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && a.allowedHost(u.Host)
}
func (a *App) authorized(r *http.Request, mcp bool) bool {
	if t := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && t != "" && len(t) == len(a.Token) && subtle.ConstantTimeCompare([]byte(t), []byte(a.Token)) == 1 {
		return true
	}
	if mcp {
		return false
	}
	c, e := r.Cookie("workspace_session")
	return e == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(a.session)) == 1
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	a.assistantRoutes(mux)
	a.desktopRoutes(mux)
	a.codexRoutes(mux)
	a.mediaRoutes(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "workspace_session", Value: a.session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/tools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"tools": a.Service.Tools()})
	})
	mux.HandleFunc("GET /api/registry", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.Service.Registry()) })
	mux.HandleFunc("GET /api/workspaces", func(w http.ResponseWriter, r *http.Request) {
		v, e := a.Service.Execute(r.Context(), "workspace_list", []byte("{}"))
		a.reply(w, v, e)
	})
	mux.HandleFunc("GET /api/workspaces/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := a.Service.Snapshot(r.PathValue("id"))
		a.reply(w, v, e)
	})
	mux.HandleFunc("GET /api/workspaces/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		v, e := a.Service.History(r.Context(), r.PathValue("id"))
		a.reply(w, v, e)
	})
	mux.HandleFunc("POST /api/commands", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "application/json required", 415)
			return
		}
		var cmd struct {
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
		d.DisallowUnknownFields()
		if e := d.Decode(&cmd); e != nil {
			var size *http.MaxBytesError
			if errors.As(e, &size) {
				http.Error(w, "request too large", 413)
			} else {
				http.Error(w, "invalid command JSON", 400)
			}
			return
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			http.Error(w, "expected a single JSON object", 400)
			return
		}
		if len(cmd.Arguments) == 0 {
			cmd.Arguments = json.RawMessage("{}")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		v, e := a.Service.Execute(ctx, cmd.Tool, cmd.Arguments)
		a.reply(w, v, e)
	})
	mux.HandleFunc("/api/", http.NotFound)
	mux.HandleFunc("GET /ws", a.websocket)
	mux.Handle("/mcp", a.MCP)
	mux.HandleFunc("/", a.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; media-src 'self' blob: https:; connect-src 'self' ws://127.0.0.1:"+a.port+" ws://localhost:"+a.port+"; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Cache-Control", "no-store")
		if !a.allowedHost(r.Host) || !a.originOK(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "origin or host rejected", 403)
			return
		}
		sensitive := strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/session" || r.URL.Path == "/ws" || r.URL.Path == "/mcp"
		if sensitive && !a.authorized(r, r.URL.Path == "/mcp") {
			w.Header().Set("WWW-Authenticate", "Bearer realm=\"ai-workspace\"")
			http.Error(w, "authentication required", 401)
			return
		}
		if r.URL.Path == "/ws" && r.Header.Get("Origin") == "" {
			http.Error(w, "websocket Origin required", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != "GET" && r.Header.Get("X-Workspace-Request") != "1" {
			http.Error(w, "X-Workspace-Request header required", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (a *App) reply(w http.ResponseWriter, v any, e error) {
	if e == nil {
		writeJSON(w, 200, v)
		return
	}
	status := 400
	message := e.Error()
	if errors.Is(e, workspace.ErrNotFound) {
		status = 404
	}
	if strings.Contains(message, "sqlite:") || strings.Contains(message, "persist command:") {
		status = 500
		message = "storage operation failed"
		a.logger.Error("storage operation failed", "error", e)
	}
	writeJSON(w, status, map[string]string{"error": message})
}
func (a *App) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "method not allowed", 405)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if strings.HasPrefix(name, "api/") || strings.HasPrefix(name, "assets/") && strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	if name == "" {
		name = "index.html"
	}
	if _, e := fs.Stat(a.assets, name); e != nil {
		if path.Ext(name) != "" || strings.HasPrefix(name, "api/") {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	}
	file, e := a.assets.Open(name)
	if e != nil {
		http.Error(w, "frontend not built; run npm run build", 503)
		return
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil {
		http.Error(w, "asset unavailable", 500)
		return
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if rs, ok := file.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, info.ModTime(), rs)
	} else {
		b, e := io.ReadAll(file)
		if e != nil {
			http.Error(w, "asset unavailable", 500)
			return
		}
		http.ServeContent(w, r, name, info.ModTime(), strings.NewReader(string(b)))
	}
}
