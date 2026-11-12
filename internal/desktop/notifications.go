// Package desktop delivers quiet local macOS Notification Center messages.
// Notification text is always data passed as argv, never executable script.
package desktop

import (
	"aiworkspace/internal/workspace"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

var ErrUnsupported = errors.New("Системные уведомления доступны только на macOS.")

const script = `on run argv
 display notification (item 2 of argv) with title (item 1 of argv) subtitle (item 3 of argv)
end run`

type Notification struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Subtitle string `json:"subtitle,omitempty"`
}
type Driver interface {
	Supported() bool
	Deliver(context.Context, Notification) error
}
type MacOS struct {
	Platform string
	Run      func(context.Context, string, ...string) error
}

func NewMacOS() *MacOS {
	return &MacOS{Platform: runtime.GOOS, Run: func(ctx context.Context, path string, args ...string) error {
		return exec.CommandContext(ctx, path, args...).Run()
	}}
}
func (d *MacOS) Supported() bool { return d.Platform == "darwin" }
func (d *MacOS) Deliver(ctx context.Context, n Notification) error {
	if !d.Supported() {
		return ErrUnsupported
	}
	if e := validate(n); e != nil {
		return e
	}
	if e := d.Run(ctx, "/usr/bin/osascript", "-e", script, "--", trim(n.Title, 100), trim(n.Message, 400), trim(n.Subtitle, 100)); e != nil {
		return fmt.Errorf("Не удалось отправить уведомление macOS. Проверьте доступность osascript в пользовательской графической сессии и настройки Script Editor: %w", e)
	}
	return nil
}
func trim(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return string(r)
}
func validate(n Notification) error {
	if len(n.ID) == 0 || len(n.ID) > 240 || strings.TrimSpace(n.Message) == "" || len([]rune(n.Message)) > 8000 || len([]rune(n.Title)) > 200 || len([]rune(n.Subtitle)) > 200 || strings.ContainsRune(n.Message+n.Title+n.Subtitle, 0) {
		return errors.New("Некорректное системное уведомление.")
	}
	return nil
}

type RecordStore interface {
	PutRecord(context.Context, string, json.RawMessage) error
	ClaimRecord(context.Context, string, json.RawMessage) (bool, error)
}
type Status struct {
	Supported  bool   `json:"supported"`
	Engine     string `json:"engine"`
	LastStatus string `json:"last_status"`
	LastError  string `json:"last_error,omitempty"`
	Note       string `json:"note"`
}
type job struct {
	notification Notification
	key          string
}
type Manager struct {
	mu          sync.Mutex
	driver      Driver
	store       RecordStore
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	observed    chan struct{}
	jobs        chan job
	once        sync.Once
	status      Status
	attempts    []time.Time
	unsubscribe func()
}

func New(service *workspace.Service, store RecordStore, driver Driver) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	events, unsubscribe := service.SubscribeEvents()
	m := &Manager{driver: driver, store: store, ctx: ctx, cancel: cancel, done: make(chan struct{}), observed: make(chan struct{}), jobs: make(chan job, 32), unsubscribe: unsubscribe, status: Status{Supported: driver.Supported(), Engine: "macOS Notification Center", LastStatus: "idle", Note: "Баннеры и их оформление зависят от настроек macOS. Доставка в Центр уведомлений не подтверждает показ баннера; приложение отправляет уведомления без звука."}}
	go m.observe(events)
	go m.loop()
	return m
}
func (m *Manager) Status() Status { m.mu.Lock(); defer m.mu.Unlock(); return m.status }
func (m *Manager) Send(ctx context.Context, n Notification) (string, error) {
	if !m.driver.Supported() {
		return "", ErrUnsupported
	}
	if e := validate(n); e != nil {
		return "", e
	}
	if e := ctx.Err(); e != nil {
		return "", e
	}
	m.mu.Lock()
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return "", errors.New("Сервис уведомлений завершён.")
	}
	now := time.Now()
	recent := m.attempts[:0]
	for _, t := range m.attempts {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	m.attempts = recent
	// Throttle new messages while allowing duplicate HTTP deliveries to be ignored.
	hash := sha256.Sum256([]byte(n.ID))
	key := "desktop:" + hex.EncodeToString(hash[:])
	claimed, e := m.store.ClaimRecord(ctx, key, json.RawMessage(`{"status":"reserved"}`))
	if e != nil {
		m.mu.Unlock()
		return "", e
	}
	if !claimed {
		m.mu.Unlock()
		return "duplicate", nil
	}
	if len(m.attempts) >= 30 {
		m.mu.Unlock()
		_ = m.store.PutRecord(ctx, key, json.RawMessage(`{"status":"rate_limited"}`))
		return "", errors.New("Лимит системных уведомлений: 30 в минуту.")
	}
	m.attempts = append(m.attempts, now)
	select {
	case m.jobs <- job{n, key}:
		m.status.LastStatus = "queued"
		m.mu.Unlock()
		return "queued", nil
	default:
		m.mu.Unlock()
		_ = m.store.PutRecord(ctx, key, json.RawMessage(`{"status":"queue_full"}`))
		return "", errors.New("Очередь системных уведомлений заполнена.")
	}
}
func (m *Manager) observe(events <-chan workspace.Event) {
	defer close(m.observed)
	for {
		select {
		case <-m.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				if m.ctx.Err() == nil {
					m.recordError(errors.New("Подписка системных уведомлений остановлена. Проверьте историю пространства и перезапустите сервер."))
				}
				return
			}
			if event.Type != "widget.created" {
				continue
			}
			state, ok := event.Payload.(workspace.State)
			if !ok {
				continue
			}
			for _, w := range state.Widgets {
				if w.ID == event.WidgetID && w.Type == "notification" {
					message, _ := w.Config["message"].(string)
					_, err := m.Send(m.ctx, Notification{ID: fmt.Sprintf("widget:%s:%d", event.WorkspaceID, event.Version), Title: w.Title, Subtitle: "Forma · " + trim(state.Name, 80), Message: message})
					if err != nil && !errors.Is(err, ErrUnsupported) {
						m.recordError(err)
					}
					break
				}
			}
		}
	}
}
func (m *Manager) recordError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.LastStatus = "error"
	m.status.LastError = err.Error()
}
func (m *Manager) loop() {
	defer close(m.done)
	for {
		select {
		case <-m.ctx.Done():
			return
		case entry := <-m.jobs:
			ctx, cancel := context.WithTimeout(m.ctx, 4*time.Second)
			err := m.driver.Deliver(ctx, entry.notification)
			cancel()
			status := "submitted"
			if err != nil {
				status = "error"
			}
			m.mu.Lock()
			m.status.LastStatus = status
			m.status.LastError = ""
			if err != nil {
				m.status.LastError = err.Error()
			}
			m.mu.Unlock()
			raw, _ := json.Marshal(map[string]string{"status": status})
			_ = m.store.PutRecord(m.ctx, entry.key, raw)
		}
	}
}
func (m *Manager) Close() { m.once.Do(func() { m.unsubscribe(); m.cancel(); <-m.done; <-m.observed }) }
