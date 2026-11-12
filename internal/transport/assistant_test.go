package transport

import (
	"aiworkspace/internal/assistant"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type testVoiceProvider struct{}

func (*testVoiceProvider) Configured() bool { return true }
func (*testVoiceProvider) Create(context.Context, string, map[string]any) (string, string, error) {
	return "v=0\r\ns=mock-answer\r\n", "private_provider_call", nil
}
func (*testVoiceProvider) Hangup(context.Context, string) error { return nil }
func TestAssistantHTTPAndWorkspacePush(t *testing.T) {
	a, ts := testApp(t, &testVoiceProvider{})
	headers := auth()
	status, _, b := request(t, ts, "GET", "/api/assistant/status", "", headers)
	if status != 200 || !strings.Contains(string(b), `"configured":true`) {
		t.Fatal(status, string(b))
	}
	_, r := dialWS(t, ts, "default")
	readWSEvent(t, r)
	status, _, b = request(t, ts, "POST", "/api/assistant/sessions", `{"workspace_id":"default","sdp":"v=0\r\n"}`, headers)
	if status != 201 || strings.Contains(string(b), "private_provider_call") {
		t.Fatal(status, string(b))
	}
	var reply assistant.SessionReply
	if e := json.Unmarshal(b, &reply); e != nil {
		t.Fatal(e)
	}
	id := reply.Session.ID
	status, _, b = request(t, ts, "POST", "/api/assistant/sessions/"+id+"/tools", `{"call_id":"call-1","name":"math_create_problem","arguments":{"kind":"linear"}}`, headers)
	if status != 200 || !strings.Contains(string(b), `"status":"success"`) {
		t.Fatal(status, string(b))
	}
	event := readWSEvent(t, r)
	if event.Type != "widget.created" {
		t.Fatal("voice command did not use dispatcher", event.Type)
	}
	st, _ := a.Service.Snapshot("default")
	if len(st.Widgets) != 1 {
		t.Fatal("voice command not saved")
	}
	for _, path := range []string{"/api/assistant/sessions/" + id + "/context", "/api/assistant/workspaces/default"} {
		status, _, _ = request(t, ts, "GET", path, "", headers)
		if status != 200 {
			t.Fatal(path, status)
		}
	}
	settings := `{"mode":"notifications","notifications":true,"language":"ru","save_history":false}`
	status, _, b = request(t, ts, "PUT", "/api/assistant/workspaces/default/settings", settings, headers)
	if status != 200 {
		t.Fatal(status, string(b))
	}
	status, _, _ = request(t, ts, "POST", "/api/assistant/sessions/"+id+"/messages", `{"id":"u1","role":"user","text":"Задача"}`, headers)
	if status != 200 {
		t.Fatal(status)
	}
	status, _, _ = request(t, ts, "DELETE", "/api/assistant/sessions/"+id, "", headers)
	if status != 204 {
		t.Fatal(status)
	}
	status, _, _ = request(t, ts, "GET", "/api/assistant/sessions/"+id+"/context", "", headers)
	if status != 404 {
		t.Fatal(status)
	}
}
func TestAssistantAuthMissingKeyAndValidation(t *testing.T) {
	_, ts := testApp(t)
	headers := auth()
	status, _, b := request(t, ts, "POST", "/api/assistant/sessions", `{"workspace_id":"default","sdp":"v=0"}`, headers)
	if status != 503 || !strings.Contains(string(b), "OPENAI_API_KEY") {
		t.Fatal(status, string(b))
	}
	status, _, _ = request(t, ts, "GET", "/api/assistant/status", "", nil)
	if status != 401 {
		t.Fatal(status)
	}
	headers["Origin"] = "http://evil.example"
	status, _, _ = request(t, ts, "GET", "/api/assistant/status", "", headers)
	if status != 403 {
		t.Fatal(status)
	}
	delete(headers, "Origin")
	status, _, _ = request(t, ts, "POST", "/api/assistant/sessions", `{"sdp":"v=0","model":"injected"}`, headers)
	if status != 400 {
		t.Fatal(status)
	}
	status, _, _ = request(t, ts, "PUT", "/api/assistant/workspaces/default/settings", `{"mode":"invalid"}`, headers)
	if status != 400 {
		t.Fatal(status)
	}
}
