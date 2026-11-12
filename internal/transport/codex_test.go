package transport

import (
	"aiworkspace/internal/codexbridge"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func (r *desktopReceipts) GetRecord(_ context.Context, k string) (json.RawMessage, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.records[k]
	return v, ok, nil
}
func TestCodexHTTPAuthValidationAndFailure(t *testing.T) {
	a, ts := testApp(t)
	a.Codex = codexbridge.New(a.Assistant, &desktopReceipts{records: map[string]json.RawMessage{}}, nil, "/usr/bin/false", t.TempDir(), t.TempDir())
	headers := auth()
	base := "/api/assistant/codex/workspaces/default"
	status, _, b := request(t, ts, "GET", "/api/assistant/status", "", headers)
	if status != 200 || !strings.Contains(string(b), `"provider":"codex"`) {
		t.Fatal(status, string(b))
	}
	status, _, _ = request(t, ts, "GET", base+"/state", "", nil)
	if status != 401 {
		t.Fatal(status)
	}
	headers["Origin"] = "https://evil.example"
	status, _, _ = request(t, ts, "POST", base+"/turns", `{}`, headers)
	if status != 403 {
		t.Fatal(status)
	}
	delete(headers, "Origin")
	for _, body := range []string{`{"request_id":"id","text":"hi","model":"injected"}`, `{"text":"hi"}`, `{"request_id":"id","text":""}`} {
		status, _, _ = request(t, ts, "POST", base+"/turns", body, headers)
		if status != 400 {
			t.Fatal(status, body)
		}
	}
	status, _, b = request(t, ts, "POST", base+"/turns", `{"request_id":"id","text":"Привет"}`, headers)
	if status != 202 {
		t.Fatal(status, string(b))
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state := a.Codex.Snapshot("default")
		if state.Status == "error" {
			if state.Error == "" {
				t.Fatal(state)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if a.Codex.Snapshot("default").Status != "error" {
		t.Fatal("process failure not reported")
	}
	status, _, body := request(t, ts, "GET", "/api/assistant/workspaces/default", "", headers)
	if status != 200 || !strings.Contains(string(body), "Привет") {
		t.Fatal("recognized input was lost on startup failure", status, string(body))
	}
	status, _, _ = request(t, ts, "POST", "/api/assistant/sessions", `{}`, headers)
	if status != 410 {
		t.Fatal("Realtime route remained enabled", status)
	}
	status, _, _ = request(t, ts, "POST", base+"/decision", `{"call_id":"wrong","approve":true}`, headers)
	if status != 400 {
		t.Fatal(status)
	}
}
