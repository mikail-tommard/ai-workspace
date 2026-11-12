package desktop

import (
	"aiworkspace/internal/workspace"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeDriver struct {
	sent chan Notification
	err  error
}

func (d *fakeDriver) Supported() bool                                 { return true }
func (d *fakeDriver) Deliver(_ context.Context, n Notification) error { d.sent <- n; return d.err }
func setup(t *testing.T, failure error) (*Manager, *workspace.Service, *workspace.Store, *fakeDriver) {
	t.Helper()
	db, e := workspace.Open(filepath.Join(t.TempDir(), "native.db"))
	if e != nil {
		t.Fatal(e)
	}
	svc, e := workspace.New(context.Background(), db)
	if e != nil {
		t.Fatal(e)
	}
	driver := &fakeDriver{sent: make(chan Notification, 64), err: failure}
	m := New(svc, db, driver)
	t.Cleanup(func() { m.Close(); svc.Close(); db.Close() })
	return m, svc, db, driver
}
func receive(t *testing.T, d *fakeDriver) Notification {
	t.Helper()
	select {
	case n := <-d.sent:
		return n
	case <-time.After(time.Second):
		t.Fatal("no native delivery")
		return Notification{}
	}
}
func TestMacOSPassesDataAsArgumentsWithoutSound(t *testing.T) {
	injected := `\" & do shell script "touch /tmp/unwanted" & "` + "\nПривет 🍀"
	d := &MacOS{Platform: "darwin"}
	d.Run = func(_ context.Context, path string, args ...string) error {
		if path != "/usr/bin/osascript" || len(args) != 6 || args[1] != script || args[2] != "--" || args[4] != injected {
			t.Fatalf("script/data mixed: %s %#v", path, args)
		}
		if strings.Contains(script, "sound") || strings.Contains(script, "shell") {
			t.Fatal("unexpected execution or sound")
		}
		return nil
	}
	if e := d.Deliver(context.Background(), Notification{ID: "x", Title: "Forma AI", Message: injected}); e != nil {
		t.Fatal(e)
	}
	d.Platform = "linux"
	if !errors.Is(d.Deliver(context.Background(), Notification{}), ErrUnsupported) {
		t.Fatal("unsupported OS accepted")
	}
	if len([]rune(trim(strings.Repeat("Ж", 500), 400))) != 400 {
		t.Fatal("rune clipping failed")
	}
}
func TestValidationAndDeliveryFailure(t *testing.T) {
	m, _, _, d := setup(t, errors.New("OS denied"))
	ctx := context.Background()
	for _, n := range []Notification{{Message: "empty id"}, {ID: "x"}, {ID: "x", Message: "bad\x00"}, {ID: "x", Message: strings.Repeat("a", 8001)}} {
		if _, e := m.Send(ctx, n); e == nil {
			t.Fatal("invalid message accepted")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := m.Send(cancelled, Notification{ID: "cancel", Message: "no"}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := m.Send(ctx, Notification{ID: "failure", Message: "test"}); e != nil {
		t.Fatal(e)
	}
	receive(t, d)
	deadline := time.Now().Add(time.Second)
	for m.Status().LastStatus != "error" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(m.Status().LastError, "OS denied") {
		t.Fatal(m.Status())
	}
}
func TestCommittedMCPNotificationAndDurableDeduplication(t *testing.T) {
	m, svc, db, d := setup(t, nil)
	ctx := context.Background()
	if _, e := svc.Execute(ctx, "ui_show_notification", []byte(`{"workspace_id":"default","message":"Готово из MCP","title":"Forma"}`)); e != nil {
		t.Fatal(e)
	}
	n := receive(t, d)
	if n.Message != "Готово из MCP" {
		t.Fatal(n)
	}
	if _, e := svc.Execute(ctx, "ui_show_notification", []byte(`{"message":""}`)); e == nil {
		t.Fatal("invalid command accepted")
	}
	// HTTP retries and a manager restart must not show the same message twice.
	if status, e := m.Send(ctx, n); e != nil || status != "duplicate" {
		t.Fatal(status, e)
	}
	m.Close()
	next := New(svc, db, d)
	defer next.Close()
	if status, e := next.Send(ctx, n); e != nil || status != "duplicate" {
		t.Fatal(status, e)
	}
	select {
	case duplicate := <-d.sent:
		t.Fatal("duplicate", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
	if _, e := m.Send(ctx, Notification{ID: "closed", Message: "no"}); e == nil {
		t.Fatal("send after shutdown accepted")
	}
}
func TestRateLimit(t *testing.T) {
	m, _, _, _ := setup(t, nil)
	m.attempts = make([]time.Time, 30)
	for i := range m.attempts {
		m.attempts[i] = time.Now()
	}
	if _, e := m.Send(context.Background(), Notification{ID: "limit", Message: "no"}); e == nil {
		t.Fatal("rate limit bypassed")
	}
}
