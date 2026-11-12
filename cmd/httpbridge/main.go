// httpbridge is a test-only IPC harness for exercising the actual HTTP handler
// with an independent MCP client in environments that disallow TCP listeners.
package main

import (
	"aiworkspace/internal/transport"
	"aiworkspace/internal/workspace"
	"aiworkspace/web"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	data, e := os.MkdirTemp("", "forma-http-bridge-")
	if e != nil {
		return e
	}
	db, e := workspace.Open(filepath.Join(data, "workspace.db"))
	if e != nil {
		return e
	}
	defer db.Close()
	s, e := workspace.New(context.Background(), db)
	if e != nil {
		return e
	}
	defer s.Close()
	cwd, e := os.Getwd()
	if e != nil {
		return e
	}
	if e = s.EnableMedia(filepath.Join(data, "media"), []string{cwd}); e != nil {
		return e
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	app := transport.NewApp(s, "independent-http-test-token-0123456789", "127.0.0.1", "8080", web.Assets(), logger)
	defer app.Close()
	handler := app.Handler()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	out := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var input struct {
			ID         int               `json:"id"`
			Method     string            `json:"method"`
			URL        string            `json:"url"`
			Headers    map[string]string `json:"headers"`
			Body       string            `json:"body"`
			BodyBase64 string            `json:"bodyBase64"`
		}
		if e := json.Unmarshal(scanner.Bytes(), &input); e != nil {
			return e
		}
		requestBody := []byte(input.Body)
		if input.BodyBase64 != "" {
			requestBody, e = base64.StdEncoding.DecodeString(input.BodyBase64)
			if e != nil {
				return e
			}
		}
		req := httptest.NewRequest(input.Method, input.URL, bytes.NewReader(requestBody))
		for k, v := range input.Headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if e := out.Encode(map[string]any{"id": input.ID, "status": rec.Code, "headers": rec.Header(), "body": rec.Body.String(), "bodyBase64": base64.StdEncoding.EncodeToString(rec.Body.Bytes())}); e != nil {
			return e
		}
	}
	return scanner.Err()
}
