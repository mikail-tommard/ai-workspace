package main

import (
	"aiworkspace/internal/assistant"
	"aiworkspace/internal/codexbridge"
	"aiworkspace/internal/desktop"
	"aiworkspace/internal/transport"
	"aiworkspace/internal/workspace"
	"aiworkspace/web"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if e := run(logger); e != nil {
		logger.Error("server stopped", "error", e)
		os.Exit(1)
	}
}
func run(logger *slog.Logger) error {
	mode := flag.String("transport", env("WORKSPACE_TRANSPORT", "http"), "http or stdio (HTTP dashboard runs in both modes)")
	addr := flag.String("addr", env("WORKSPACE_ADDR", "127.0.0.1:8080"), "loopback listen address")
	data := flag.String("data-dir", env("WORKSPACE_DATA_DIR", ".ai-workspace"), "SQLite and token directory")
	dashboard := flag.Bool("dashboard", true, "start HTTP dashboard (disable only for headless stdio/CI)")
	codexTest := flag.Bool("codex-check", false, "test actual local Codex: create a math card without starting HTTP (use a separate data-dir)")
	notifyTest := flag.Bool("notify-test", false, "submit one quiet macOS notification and exit")
	flag.Parse()
	if *notifyTest {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := desktop.NewMacOS().Deliver(ctx, desktop.Notification{ID: "test", Title: "Forma AI", Subtitle: "Центр уведомлений macOS", Message: "Ответы ассистента будут появляться здесь. Голосовые ответы выключены."}); e != nil {
			return e
		}
		logger.Info("notification submitted to macOS; banner visibility depends on system settings")
		return nil
	}
	if !*dashboard && *mode != "stdio" {
		return fmt.Errorf("dashboard=false requires stdio transport")
	}
	if *mode != "http" && *mode != "stdio" {
		return fmt.Errorf("transport must be http or stdio")
	}
	host, port, e := net.SplitHostPort(*addr)
	if e != nil {
		return e
	}
	n, portErr := strconv.Atoi(port)
	if portErr != nil || n < 1 || n > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() || strings.Contains(host, ":") {
		return fmt.Errorf("only IPv4 loopback binding is supported; public access is disabled")
	}
	if e = os.MkdirAll(*data, 0700); e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, e := workspace.Open(filepath.Join(*data, "workspace.db"))
	if e != nil {
		return e
	}
	defer store.Close()
	token := os.Getenv("WORKSPACE_TOKEN")
	tokenFile := filepath.Join(*data, "mcp-token")
	if token == "" {
		b, err := os.ReadFile(tokenFile)
		if errors.Is(err, os.ErrNotExist) {
			token = workspace.ID() + workspace.ID()
			if e = os.WriteFile(tokenFile, []byte(token+"\n"), 0600); e != nil {
				return e
			}
		} else if err != nil {
			return err
		} else {
			token = strings.TrimSpace(string(b))
		}
	}
	if len(token) < 32 {
		return fmt.Errorf("WORKSPACE_TOKEN must contain at least 32 characters")
	}
	service, e := workspace.New(ctx, store)
	if e != nil {
		return e
	}
	defer service.Close()
	mediaCwd, e := os.Getwd()
	if e != nil {
		return e
	}
	homeDir, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	importRoots := []string{mediaCwd, filepath.Join(homeDir, ".codex", "generated_images")}
	if configured := os.Getenv("WORKSPACE_MEDIA_IMPORT_ROOTS"); configured != "" {
		importRoots = append(importRoots, filepath.SplitList(configured)...)
	}
	if e = service.EnableMedia(filepath.Join(*data, "media"), importRoots); e != nil {
		return e
	}
	app := transport.NewApp(service, token, host, port, web.Assets(), logger)
	app.Assistant = assistant.New(service, store, nil)
	app.Desktop = desktop.New(service, store, desktop.NewMacOS())
	cwd, e := os.Getwd()
	if e != nil {
		return e
	}
	app.Codex = codexbridge.New(app.Assistant, store, app.Desktop, os.Getenv("WORKSPACE_CODEX_BIN"), filepath.Join(*data, "codex-runtime"), cwd)
	defer app.Close()
	if *codexTest {
		requestID := workspace.ID()
		if _, err := app.Codex.Start(ctx, "default", requestID, "Создай ровно одну математическую задачу через math_create_problem, kind=linear. После успешного вызова ответь коротко по-русски."); err != nil {
			return err
		}
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		timeout := time.NewTimer(90 * time.Second)
		defer timeout.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timeout.C:
				return fmt.Errorf("Codex check timed out")
			case <-tick.C:
				state := app.Codex.Snapshot("default")
				if state.Status == "error" {
					return fmt.Errorf("%s", state.Error)
				}
				if state.Status == "completed" {
					st, err := service.Snapshot("default")
					if err != nil {
						return err
					}
					found := false
					for _, w := range st.Widgets {
						if w.Type == "math_problem" {
							found = true
						}
					}
					if !found {
						return fmt.Errorf("Codex did not create a math widget")
					}
					logger.Info("actual Codex integration check passed", "widgets", len(st.Widgets), "thread_id", state.ThreadID)
					return nil
				}
			}
		}
	}
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errCh := make(chan error, 2)
	if *dashboard {
		listener, e := net.Listen("tcp", *addr)
		if e != nil {
			return e
		}
		go func() { errCh <- server.Serve(listener) }()
	}
	if *mode == "stdio" {
		go func() { errCh <- app.MCP.Stdio(ctx, os.Stdin, os.Stdout) }()
	}
	logger.Info("workspace ready", "url", "http://"+*addr, "transport", *mode, "token_file", tokenFile, "dashboard", *dashboard)
	var result error
	select {
	case <-ctx.Done():
	case result = <-errCh:
		if errors.Is(result, http.ErrServerClosed) {
			result = nil
		}
	}
	stop()
	app.Close()
	_ = os.Stdin.Close()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := server.Shutdown(shutdown); e != nil {
		return e
	}
	return result
}
