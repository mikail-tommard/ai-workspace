package codexbridge

import (
	"aiworkspace/internal/assistant"
	"aiworkspace/internal/workspace"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A real child process speaking the published installed-version protocol.
// No model output from this helper is counted as a live Codex model test.
func TestCodexHelperProcess(t *testing.T) {
	if os.Getenv("FORMA_CODEX_HELPER") != "1" {
		return
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		os.Exit(9)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	turn := 0
	silent := false
	send := func(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
	for scanner.Scan() {
		var msg envelope
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			os.Exit(2)
		}
		if msg.Method == "initialized" {
			continue
		}
		if msg.Method == "initialize" {
			send(map[string]any{"id": msg.ID, "result": map[string]string{"userAgent": "fixture"}})
			continue
		}
		if msg.Method == "account/read" {
			send(map[string]any{"id": msg.ID, "result": map[string]any{"account": map[string]string{"type": "chatgpt"}, "requiresOpenaiAuth": true}})
			continue
		}
		if msg.Method == "thread/start" || msg.Method == "thread/resume" {
			if msg.Method == "thread/resume" && os.Getenv("FORMA_REQUIRE_NEW_THREAD") == "1" {
				os.Exit(8)
			}
			var p map[string]any
			json.Unmarshal(msg.Params, &p)
			if msg.Method == "thread/start" {
				if p["sandbox"] != "read-only" || len(p["dynamicTools"].([]any)) < 25 {
					os.Exit(3)
				}
			}
			send(map[string]any{"id": msg.ID, "result": map[string]any{"thread": map[string]string{"id": "thread-fixture"}}})
			continue
		}
		if msg.Method == "turn/start" {
			silent = strings.Contains(string(msg.Params), "no-text")
			turn++
			turnID := fmt.Sprint("turn-", turn)
			send(map[string]any{"id": msg.ID, "result": map[string]any{"turn": map[string]string{"id": turnID, "status": "inProgress"}}})
			name := "math_create_problem"
			args := map[string]any{"kind": "linear"}
			if strings.Contains(string(msg.Params), "harder") {
				name = "widget_update"
				args = map[string]any{"math": map[string]string{"difficulty": "hard"}}
			}
			if strings.Contains(string(msg.Params), "clear") {
				name = "workspace_clear"
				args = map[string]any{}
			}
			if strings.Contains(string(msg.Params), "wait") {
				continue
			}
			send(map[string]any{"id": 100 + turn, "method": "item/tool/call", "params": map[string]any{"threadId": "thread-fixture", "turnId": turnID, "callId": fmt.Sprint("call-", turn), "tool": name, "arguments": args}})
			continue
		}
		if msg.Method == "turn/interrupt" {
			send(map[string]any{"id": msg.ID, "result": map[string]any{}})
			continue
		}
		if msg.Method == "" && len(msg.ID) > 0 {
			turnID := fmt.Sprint("turn-", turn)
			if silent {
				send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread-fixture", "turn": map[string]string{"id": turnID, "status": "completed"}}})
				continue
			}
			item := map[string]string{"id": "answer", "type": "agentMessage", "text": "Готово из Codex fixture."}
			send(map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread-fixture", "item": item}})
			send(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread-fixture", "itemId": "answer", "delta": "Готово"}})
			send(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "thread-fixture", "item": item}})
			send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread-fixture", "turn": map[string]string{"id": turnID, "status": "completed"}}})
		}
	}
	os.Exit(0)
}
func setup(t *testing.T) (*Manager, *workspace.Service, *workspace.Store) {
	t.Helper()
	t.Setenv("FORMA_CODEX_HELPER", "1")
	t.Setenv("OPENAI_API_KEY", "dummy-key-must-not-be-inherited")
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "codex-fixture")
	body := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run '^TestCodexHelperProcess$' \"$@\"\n"
	if e = os.WriteFile(script, []byte(body), 0700); e != nil {
		t.Fatal(e)
	}
	db, e := workspace.Open(filepath.Join(dir, "workspace.db"))
	if e != nil {
		t.Fatal(e)
	}
	svc, e := workspace.New(context.Background(), db)
	if e != nil {
		t.Fatal(e)
	}
	a := assistant.New(svc, db, nil)
	m := New(a, db, nil, script, filepath.Join(dir, "runtime"), dir)
	t.Cleanup(func() { m.Close(); a.Close(); svc.Close(); db.Close() })
	return m, svc, db
}
func wait(t *testing.T, m *Manager, status string) State {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Snapshot("default")
		m.mu.Lock()
		idle := m.sessions["default"] == nil || m.sessions["default"].cancel == nil
		m.mu.Unlock()
		if s.Status == status && (status == "confirmation" || idle) {
			return s
		}
		if s.Status == "error" {
			t.Fatal(s)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timeout", m.Snapshot("default"))
	return State{}
}
func TestCodexProtocolToolContextDedupAndHistory(t *testing.T) {
	m, svc, _ := setup(t)
	ctx := context.Background()
	if _, e := m.Start(ctx, "default", "one", "create"); e != nil {
		t.Fatal(e)
	}
	s := wait(t, m, "completed")
	if s.Text != "Готово из Codex fixture." || s.ThreadID != "thread-fixture" {
		t.Fatal(s)
	}
	st, _ := svc.Snapshot("default")
	if len(st.Widgets) != 1 {
		t.Fatal(st)
	}
	id := st.Widgets[0].ID
	if _, e := m.Start(ctx, "default", "one", "create"); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Start(ctx, "default", "one", "different"); e == nil {
		t.Fatal("ID reused")
	}
	if _, e := m.Start(ctx, "default", "two", "harder"); e != nil {
		t.Fatal(e)
	}
	wait(t, m, "completed")
	st, _ = svc.Snapshot("default")
	if len(st.Widgets) != 1 || st.Widgets[0].ID != id || st.Widgets[0].Problem.Difficulty != "hard" {
		t.Fatal(st)
	}
	conv, e := m.assistant.Conversation(ctx, "default")
	if e != nil || len(conv.Messages) != 4 {
		t.Fatal(conv, e)
	}
}
func TestCodexConfirmationAndCancel(t *testing.T) {
	m, svc, _ := setup(t)
	ctx := context.Background()
	m.Start(ctx, "default", "one", "create")
	wait(t, m, "completed")
	m.Start(ctx, "default", "two", "clear")
	s := wait(t, m, "confirmation")
	st, _ := svc.Snapshot("default")
	if len(st.Widgets) != 1 {
		t.Fatal("cleared without approval")
	}
	if e := m.Decide("default", "wrong", true); e == nil {
		t.Fatal("wrong nonce accepted")
	}
	if e := m.Decide("default", s.Pending.ID, false); e != nil {
		t.Fatal(e)
	}
	wait(t, m, "completed")
	st, _ = svc.Snapshot("default")
	if len(st.Widgets) != 1 {
		t.Fatal("rejection ignored")
	}
	m.Start(ctx, "default", "three", "wait")
	m.mu.Lock()
	runCtx := m.sessions["default"].runCtx
	m.mu.Unlock()
	if deadline, ok := runCtx.Deadline(); ok {
		t.Fatalf("turn must wait for completion or cancellation without a deadline: %v", deadline)
	}
	if _, e := m.Start(ctx, "default", "four", "create"); e != ErrBusy {
		t.Fatal(e)
	}
	m.Cancel("default")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.Snapshot("default").Status == "error" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cancel did not stop turn")
}
func TestDurableTurnReceiptAndPrivacy(t *testing.T) {
	m, svc, db := setup(t)
	ctx := context.Background()
	settings := assistant.DefaultSettings()
	settings.SaveHistory = false
	if e := m.assistant.SetSettings(ctx, "default", settings); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Start(ctx, "default", "private", "create"); e != nil {
		t.Fatal(e)
	}
	wait(t, m, "completed")
	m.Close()
	raw, ok, e := db.GetRecord(ctx, receiptKey("default", "private"))
	if e != nil || !ok || strings.Contains(string(raw), "Готово из Codex") {
		t.Fatal("private answer persisted", string(raw), e)
	}
	next := New(m.assistant, db, nil, m.binary, m.runtime, m.cwd)
	defer next.Close()
	if _, e = next.Start(ctx, "default", "private", "create"); e != nil {
		t.Fatal(e)
	}
	st, _ := svc.Snapshot("default")
	if len(st.Widgets) != 1 {
		t.Fatal("replayed tool after restart")
	}
}

func TestToolOnlyTurnStillGetsConfirmedTextReply(t *testing.T) {
	m, _, _ := setup(t)
	if _, e := m.Start(context.Background(), "default", "silent", "no-text"); e != nil {
		t.Fatal(e)
	}
	state := wait(t, m, "completed")
	if state.Text != "Команда выполнена. Результат доступен на панели." {
		t.Fatal(state)
	}
	c, e := m.assistant.Conversation(context.Background(), "default")
	if e != nil || len(c.Messages) != 2 || c.Messages[1].Text != state.Text {
		t.Fatal(c, e)
	}
}
func TestOldToolCatalogStartsFreshCodexThread(t *testing.T) {
	m, _, db := setup(t)
	ctx := context.Background()
	if _, e := m.Start(ctx, "default", "first", "create"); e != nil {
		t.Fatal(e)
	}
	wait(t, m, "completed")
	m.Close()
	if e := db.PutRecord(ctx, "codex-thread:default", json.RawMessage(`{"id":"thread-fixture","schema":"old-tools"}`)); e != nil {
		t.Fatal(e)
	}
	t.Setenv("FORMA_REQUIRE_NEW_THREAD", "1")
	next := New(m.assistant, db, nil, m.binary, m.runtime, m.cwd)
	defer next.Close()
	if _, e := next.Start(ctx, "default", "second", "create"); e != nil {
		t.Fatal(e)
	}
	wait(t, next, "completed")
}
