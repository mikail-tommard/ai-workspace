// Package assistant owns voice sessions and adapts the shared workspace command
// service. Audio travels directly over WebRTC and is never stored by this module.
package assistant

import (
	"aiworkspace/internal/workspace"
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrUnavailable = errors.New("Ассистент не настроен: задайте OPENAI_API_KEY на сервере и перезапустите приложение.")
var ErrSession = errors.New("Сессия ассистента завершена или не найдена. Подключитесь снова.")
var ErrRateLimit = errors.New("Слишком много подключений. Подождите минуту.")

type Settings struct {
	Mode          string `json:"mode"`
	Notifications bool   `json:"notifications"`
	Language      string `json:"language"`
	SaveHistory   bool   `json:"save_history"`
	SpeechPauseMS int    `json:"speech_pause_ms"`
}

func DefaultSettings() Settings {
	return Settings{Mode: "notifications", Notifications: true, Language: "ru", SaveHistory: true, SpeechPauseMS: 4000}
}
func SettingsSchema() *workspace.Schema {
	return workspace.Obj(map[string]*workspace.Schema{"speech_pause_ms": workspace.Integer(2000, 10000), "mode": workspace.Enum("notifications"), "notifications": {Type: "boolean"}, "language": workspace.Enum("ru", "en"), "save_history": {Type: "boolean"}}, "mode", "notifications", "language", "save_history")
}

type Message struct {
	ID   string    `json:"id"`
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}
type Conversation struct {
	Settings   Settings  `json:"settings"`
	Messages   []Message `json:"messages"`
	LastWidget string    `json:"last_widget_id,omitempty"`
}
type RecordStore interface {
	GetRecord(context.Context, string) (json.RawMessage, bool, error)
	PutRecord(context.Context, string, json.RawMessage) error
	ClaimRecord(context.Context, string, json.RawMessage) (bool, error)
}
type Provider interface {
	Configured() bool
	Create(context.Context, string, map[string]any) (string, string, error)
	Hangup(context.Context, string) error
}
type Function struct {
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  *workspace.Schema `json:"parameters"`
}
type Session struct {
	ID          string             `json:"id"`
	WorkspaceID string             `json:"workspace_id"`
	Expires     time.Time          `json:"expires_at"`
	CallID      string             `json:"-"`
	Cancel      context.CancelFunc `json:"-"`
}
type SessionReply struct {
	Session Session        `json:"session"`
	SDP     string         `json:"sdp"`
	Context map[string]any `json:"context"`
}
type ToolCall struct {
	CallID       string          `json:"call_id"`
	Name         string          `json:"name"`
	Arguments    json.RawMessage `json:"arguments"`
	Confirmation string          `json:"confirmation,omitempty"`
	Decision     string          `json:"decision,omitempty"`
}
type ToolReply struct {
	Status       string           `json:"status"`
	Result       workspace.Result `json:"result,omitempty"`
	Error        string           `json:"error,omitempty"`
	Confirmation string           `json:"confirmation,omitempty"`
	Label        string           `json:"label,omitempty"`
}
