package transport

import (
	"aiworkspace/internal/assistant"
	"aiworkspace/internal/desktop"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

type desktopReceipts struct {
	mu      sync.Mutex
	records map[string]json.RawMessage
}

func (r *desktopReceipts) PutRecord(_ context.Context, k string, v json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[k] = v
	return nil
}
func (r *desktopReceipts) ClaimRecord(_ context.Context, k string, v json.RawMessage) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[k]; ok {
		return false, nil
	}
	r.records[k] = v
	return true, nil
}

type desktopDriver struct{ delivered chan desktop.Notification }

func (d *desktopDriver) Supported() bool { return true }
func (d *desktopDriver) Deliver(_ context.Context, n desktop.Notification) error {
	d.delivered <- n
	return nil
}
func TestNativeNotificationAuthValidationAndAssistantDelivery(t *testing.T) {
	a, ts := testApp(t, &testVoiceProvider{})
	driver := &desktopDriver{delivered: make(chan desktop.Notification, 10)}
	a.Desktop = desktop.New(a.Service, &desktopReceipts{records: map[string]json.RawMessage{}}, driver)
	headers := auth()
	if status, _, _ := request(t, ts, "GET", "/api/desktop/status", "", nil); status != 401 {
		t.Fatal(status)
	}
	headers["Origin"] = "http://evil.example"
	if status, _, _ := request(t, ts, "POST", "/api/desktop/notifications/test", "{}", headers); status != 403 {
		t.Fatal(status)
	}
	delete(headers, "Origin")
	for _, body := range []string{`{"id":"x","message":""}`, `{"id":"x","message":"hi","script":"unsafe"}`, `{"message":"hi"}`} {
		if status, _, _ := request(t, ts, "POST", "/api/desktop/notifications", body, headers); status != 400 {
			t.Fatal(body, status)
		}
	}
	status, _, b := request(t, ts, "POST", "/api/assistant/sessions", `{"workspace_id":"default","sdp":"v=0"}`, headers)
	if status != 201 {
		t.Fatal(status, string(b))
	}
	var session assistant.SessionReply
	json.Unmarshal(b, &session)
	path := "/api/assistant/sessions/" + session.Session.ID + "/messages"
	// User transcripts do not trigger notifications; an assistant response does.
	status, _, _ = request(t, ts, "POST", path, `{"id":"u","role":"user","text":"Создай задачу"}`, headers)
	if status != 200 {
		t.Fatal(status)
	}
	body := `{"id":"r","role":"assistant","text":"Готово! Задача создана."}`
	for i := 0; i < 2; i++ {
		status, _, b = request(t, ts, "POST", path, body, headers)
		if status != 200 {
			t.Fatal(status, string(b))
		}
	}
	select {
	case n := <-driver.delivered:
		if n.Message != "Готово! Задача создана." || n.Title != "Forma AI" {
			t.Fatal(n)
		}
	case <-time.After(time.Second):
		t.Fatal("response not delivered")
	}
	select {
	case n := <-driver.delivered:
		t.Fatal("duplicate or user notification", n)
	case <-time.After(20 * time.Millisecond):
	}
	settings := `{"mode":"notifications","notifications":false,"language":"ru","save_history":true}`
	status, _, _ = request(t, ts, "PUT", "/api/assistant/workspaces/default/settings", settings, headers)
	if status != 200 {
		t.Fatal(status)
	}
	status, _, b = request(t, ts, "POST", path, `{"id":"r2","role":"assistant","text":"Только история"}`, headers)
	if status != 200 || !strings.Contains(string(b), `"notification_status":"disabled"`) {
		t.Fatal(status, string(b))
	}
	status, _, _ = request(t, ts, "POST", "/api/desktop/notifications/test", "{}", headers)
	if status != 202 {
		t.Fatal(status)
	}
	select {
	case n := <-driver.delivered:
		if n.Subtitle != "Центр уведомлений macOS" {
			t.Fatal(n)
		}
	case <-time.After(time.Second):
		t.Fatal("test not delivered")
	}
}
