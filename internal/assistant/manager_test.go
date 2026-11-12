package assistant

import (
	"aiworkspace/internal/workspace"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type mockProvider struct {
	mu      sync.Mutex
	creates int
	hangups int
	config  map[string]any
	failure error
	wait    bool
}

func (p *mockProvider) Configured() bool { return true }
func (p *mockProvider) Create(ctx context.Context, _ string, config map[string]any) (string, string, error) {
	p.mu.Lock()
	p.creates++
	p.config = config
	wait, err := p.wait, p.failure
	p.mu.Unlock()
	if wait {
		<-ctx.Done()
		return "", "", ctx.Err()
	}
	if err != nil {
		return "", "", err
	}
	return "v=0\r\ns=mock\r\n", "rtc_mock", nil
}
func (p *mockProvider) Hangup(context.Context, string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hangups++
	return nil
}
func setup(t *testing.T) (*Manager, *workspace.Service, *workspace.Store, *mockProvider) {
	t.Helper()
	db, e := workspace.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	s, e := workspace.New(context.Background(), db)
	if e != nil {
		t.Fatal(e)
	}
	p := &mockProvider{}
	m := New(s, db, p)
	t.Cleanup(func() { m.Close(); s.Close(); db.Close() })
	return m, s, db, p
}
func session(t *testing.T, m *Manager) string {
	t.Helper()
	r, e := m.Create(context.Background(), "owner", "default", "v=0\r\ns=offer\r\n")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(r.Session.ID, "rtc_") {
		t.Fatal("provider call leaked")
	}
	return r.Session.ID
}
func tool(t *testing.T, m *Manager, id, name string, args any) ToolReply {
	t.Helper()
	b, _ := json.Marshal(args)
	r, e := m.Call(context.Background(), "owner", id, ToolCall{CallID: workspace.ID(), Name: name, Arguments: b})
	if e != nil {
		t.Fatal(e)
	}
	if r.Status != "success" {
		t.Fatalf("%s: %#v", name, r)
	}
	return r
}
func TestCreateAndConfiguration(t *testing.T) {
	m, _, _, p := setup(t)
	id := session(t, m)
	if _, e := m.SessionWorkspace("other", id); !errors.Is(e, ErrSession) {
		t.Fatal("session ownership not checked")
	}
	p.mu.Lock()
	b, _ := json.Marshal(p.config)
	p.mu.Unlock()
	text := string(b)
	for _, v := range []string{`"type":"realtime"`, `"output_modalities":["text"]`, `"create_response":false`, `widget_update`, `last_widget_id`} {
		if !strings.Contains(text, v) {
			t.Fatal("missing config", v)
		}
	}
	if strings.Contains(text, `"output":`) || strings.Contains(text, `"voice":`) {
		t.Fatal("speech output configured", text)
	}
	if e := m.End(context.Background(), "other", id); !errors.Is(e, ErrSession) {
		t.Fatal(e)
	}
	if e := m.End(context.Background(), "owner", id); e != nil {
		t.Fatal(e)
	}
	if _, e := m.SessionWorkspace("owner", id); !errors.Is(e, ErrSession) {
		t.Fatal(e)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hangups != 1 {
		t.Fatal(p.hangups)
	}
}
func TestContextAndInPlaceMath(t *testing.T) {
	m, s, _, _ := setup(t)
	id := session(t, m)
	r := tool(t, m, id, "math_create_problem", map[string]any{"kind": "quadratic", "difficulty": "easy"})
	w := r.Result["widget"].(map[string]any)
	originalID := w["id"].(string)
	r = tool(t, m, id, "widget_update", map[string]any{"math": map[string]any{"difficulty": "hard"}})
	updated := r.Result["widget"].(map[string]any)
	if updated["id"] != originalID || updated["problem"].(map[string]any)["difficulty"] != "hard" {
		t.Fatal("task replaced or not updated")
	}
	st, _ := s.Snapshot("default")
	if len(st.Widgets) != 1 || st.Widgets[0].ID != originalID {
		t.Fatal("new random widget created")
	}
	tool(t, m, id, "math_show_solution", map[string]any{})
	st, _ = s.Snapshot("default")
	if !st.Widgets[0].Problem.Revealed {
		t.Fatal("wrong problem revealed")
	}
	c, e := m.Context(context.Background(), "default")
	if e != nil || c["last_widget_id"] != originalID {
		t.Fatal(c, e)
	}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), "expected") {
		t.Fatal("answers leaked")
	}
}
func TestAtMostOnceAndPayloadBinding(t *testing.T) {
	m, s, _, _ := setup(t)
	id := session(t, m)
	call := ToolCall{CallID: "same-call", Name: "math_create_problem", Arguments: json.RawMessage(`{"kind":"linear"}`)}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := m.Call(context.Background(), "owner", id, call)
			if e != nil {
				errs <- e
			} else if r.Status != "success" {
				errs <- fmt.Errorf("%s", r.Error)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	st, _ := s.Snapshot("default")
	if len(st.Widgets) != 1 {
		t.Fatal("duplicate side effects")
	}
	call.Arguments = json.RawMessage(`{"kind":"fraction"}`)
	r, e := m.Call(context.Background(), "owner", id, call)
	if e != nil || r.Status != "error" {
		t.Fatal(r, e)
	}
}
func TestDestructiveConfirmationAndVersion(t *testing.T) {
	m, s, _, _ := setup(t)
	id := session(t, m)
	tool(t, m, id, "math_create_problem", map[string]any{})
	call := ToolCall{CallID: "clear-1", Name: "workspace_clear", Arguments: json.RawMessage(`{}`)}
	r, e := m.Call(context.Background(), "owner", id, call)
	if e != nil || r.Status != "confirmation_required" {
		t.Fatal(r, e)
	}
	st, _ := s.Snapshot("default")
	if len(st.Widgets) != 1 {
		t.Fatal("deleted without confirmation")
	}
	call.Decision = "approve"
	call.Confirmation = "invented"
	r, e = m.Call(context.Background(), "owner", id, call)
	if e != nil || r.Status != "error" {
		t.Fatal(r, e)
	}
	call.Decision = ""
	call.Confirmation = ""
	r, _ = m.Call(context.Background(), "owner", id, call)
	nonce := r.Confirmation
	tool(t, m, id, "text_create", map[string]any{"title": "New", "content": "changed after preview"})
	call.Decision = "approve"
	call.Confirmation = nonce
	r, e = m.Call(context.Background(), "owner", id, call)
	if e != nil || r.Status != "error" {
		t.Fatal("stale confirmation accepted", r, e)
	}
	st, _ = s.Snapshot("default")
	if len(st.Widgets) != 2 {
		t.Fatal("stale confirmation deleted widgets")
	}
	call = ToolCall{CallID: "clear-2", Name: "workspace_clear", Arguments: json.RawMessage(`{}`)}
	r, _ = m.Call(context.Background(), "owner", id, call)
	call.Decision = "approve"
	call.Confirmation = r.Confirmation
	r, e = m.Call(context.Background(), "owner", id, call)
	if e != nil || r.Status != "success" {
		t.Fatal(r, e)
	}
	st, _ = s.Snapshot("default")
	if len(st.Widgets) != 0 {
		t.Fatal("confirmed clear not applied")
	}
}
func TestRejectValidationAndWorkspaceIsolation(t *testing.T) {
	m, _, _, _ := setup(t)
	id := session(t, m)
	for _, raw := range []string{`{"workspace_id":"other"}`, `{"unknown":true}`, `null`, `[]`} {
		r, e := m.Call(context.Background(), "owner", id, ToolCall{CallID: workspace.ID(), Name: "math_create_problem", Arguments: json.RawMessage(raw)})
		if e != nil || r.Status != "error" {
			t.Fatal(raw, r, e)
		}
	}
	call := ToolCall{CallID: "reject", Name: "workspace_clear", Arguments: json.RawMessage(`{}`)}
	r, _ := m.Call(context.Background(), "owner", id, call)
	call.Decision = "reject"
	call.Confirmation = r.Confirmation
	r, e := m.Call(context.Background(), "owner", id, call)
	if e != nil || r.Status != "cancelled" {
		t.Fatal(r, e)
	}
}
func TestHistorySettingsAndReload(t *testing.T) {
	m, s, db, p := setup(t)
	ctx := context.Background()
	msg := Message{ID: "user-1", Role: "user", Text: "Создай задачу"}
	if e := m.Append(ctx, "default", msg); e != nil {
		t.Fatal(e)
	}
	if e := m.Append(ctx, "default", msg); e != nil {
		t.Fatal(e)
	}
	other := New(s, db, p)
	defer other.Close()
	c, e := other.Conversation(ctx, "default")
	if e != nil || len(c.Messages) != 1 {
		t.Fatal(c, e)
	}
	settings := c.Settings
	settings.Mode = "notifications"
	settings.SaveHistory = false
	if e = m.SetSettings(ctx, "default", settings); e != nil {
		t.Fatal(e)
	}
	if e = m.Append(ctx, "default", Message{ID: "private", Role: "user", Text: "Не сохранять"}); e != nil {
		t.Fatal(e)
	}
	reloaded := New(s, db, p)
	defer reloaded.Close()
	c, e = reloaded.Conversation(ctx, "default")
	if e != nil || len(c.Messages) != 0 || c.Settings.Mode != "notifications" {
		t.Fatal(c, e)
	}
	settings.Mode = "voice"
	if e = m.SetSettings(ctx, "default", settings); e == nil {
		t.Fatal("voice output mode accepted")
	}
}
func TestCancellationMissingKeyAndLimit(t *testing.T) {
	m, s, db, p := setup(t)
	empty := New(s, db, NewRealtimeClient("", ""))
	defer empty.Close()
	if _, e := empty.Create(context.Background(), "owner", "default", "v=0"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	p.mu.Lock()
	p.wait = true
	p.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := m.Create(ctx, "owner", "default", "v=0"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	p.mu.Lock()
	p.wait = false
	p.mu.Unlock()
	for i := 0; i < 4; i++ {
		session(t, m)
	}
	if _, e := m.Create(context.Background(), "owner", "default", "v=0"); !errors.Is(e, ErrRateLimit) {
		t.Fatal(e)
	}
}
func TestProviderFailureAndClose(t *testing.T) {
	m, _, _, p := setup(t)
	p.mu.Lock()
	p.failure = errors.New("unavailable")
	p.mu.Unlock()
	if _, e := m.Create(context.Background(), "owner", "default", "v=0"); e == nil {
		t.Fatal("failure hidden")
	}
	p.mu.Lock()
	p.failure = nil
	p.mu.Unlock()
	session(t, m)
	m.Close()
	m.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hangups != 1 {
		t.Fatal(p.hangups)
	}
}

func TestLongConversationHasDurableByteBudget(t *testing.T) {
	m, s, db, p := setup(t)
	ctx := context.Background()
	text := strings.Repeat("Д", 7900)
	for i := 0; i < 45; i++ {
		if e := m.Append(ctx, "default", Message{ID: fmt.Sprint("long-", i), Role: "user", Text: text}); e != nil {
			t.Fatal(e)
		}
	}
	reloaded := New(s, db, p)
	defer reloaded.Close()
	c, e := reloaded.Conversation(ctx, "default")
	if e != nil || len(c.Messages) == 0 || len(c.Messages) >= 45 {
		t.Fatal("history byte budget was not applied", len(c.Messages), e)
	}
	if c.Messages[len(c.Messages)-1].ID != "long-44" {
		t.Fatal("newest history lost")
	}
}

func TestLegacySpeechSettingsBecomeNotifications(t *testing.T) {
	m, _, db, _ := setup(t)
	raw := json.RawMessage(`{"settings":{"mode":"both","voice":"marin","volume":1,"duration_ms":5000,"notifications":true,"language":"ru","save_history":true},"messages":[]}`)
	if e := db.PutRecord(context.Background(), "conversation:default", raw); e != nil {
		t.Fatal(e)
	}
	c, e := m.Conversation(context.Background(), "default")
	if e != nil || c.Settings.Mode != "notifications" {
		t.Fatal(c, e)
	}
	b, _ := json.Marshal(c.Settings)
	if strings.Contains(string(b), "voice") || strings.Contains(string(b), "volume") {
		t.Fatal("obsolete settings leaked", string(b))
	}
}
