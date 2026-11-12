package transport

import (
	"aiworkspace/internal/assistant"
	"aiworkspace/internal/desktop"
	"aiworkspace/internal/workspace"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

func decodeAssistant(w http.ResponseWriter, r *http.Request, dest any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("Требуется Content-Type: application/json.")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	d.DisallowUnknownFields()
	if e := d.Decode(dest); e != nil {
		return errors.New("Некорректный JSON или превышен размер запроса.")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("Ожидается один JSON-объект.")
	}
	return nil
}
func assistantError(w http.ResponseWriter, e error) {
	status := 400
	switch {
	case errors.Is(e, assistant.ErrUnavailable):
		status = 503
	case errors.Is(e, assistant.ErrSession), errors.Is(e, workspace.ErrNotFound):
		status = 404
	case errors.Is(e, assistant.ErrRateLimit):
		status = 429
	case errors.Is(e, context.Canceled), errors.Is(e, context.DeadlineExceeded):
		status = 408
	}
	message := e.Error()
	if len(message) > 400 {
		message = "Внутренняя ошибка ассистента."
	}
	writeJSON(w, status, map[string]string{"error": message})
}

// The application has one local owner. Cookie/Bearer authorization and Origin
// checks are applied before these handlers; session IDs are additional capabilities.
func (a *App) assistantRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/assistant/status", func(w http.ResponseWriter, r *http.Request) {
		configured := a.Assistant != nil && a.Assistant.Configured()
		if a.Codex != nil {
			configured = a.Codex.Available()
			message := ""
			if !configured {
				message = "Codex CLI не найден. Установите Codex и выполните codex login на этом Mac."
			}
			writeJSON(w, 200, map[string]any{"configured": configured, "message": message, "provider": "codex", "default_model": "Настройки локального Codex"})
			return
		}
		message := ""
		if !configured {
			message = assistant.ErrUnavailable.Error()
		}
		writeJSON(w, 200, map[string]any{"configured": configured, "message": message, "default_model": assistant.DefaultModel})
	})
	ready := func(w http.ResponseWriter) bool {
		if a.Assistant == nil {
			assistantError(w, assistant.ErrUnavailable)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/assistant/workspaces/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		c, e := a.Assistant.Conversation(r.Context(), r.PathValue("id"))
		if e != nil {
			assistantError(w, e)
			return
		}
		writeJSON(w, 200, c)
	})
	mux.HandleFunc("PUT /api/assistant/workspaces/{id}/settings", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var settings assistant.Settings
		if e := decodeAssistant(w, r, &settings); e != nil {
			assistantError(w, e)
			return
		}
		if e := a.Assistant.SetSettings(r.Context(), r.PathValue("id"), settings); e != nil {
			assistantError(w, e)
			return
		}
		conversation, e := a.Assistant.Conversation(r.Context(), r.PathValue("id"))
		if e != nil {
			assistantError(w, e)
			return
		}
		writeJSON(w, 200, conversation.Settings)
	})
	mux.HandleFunc("POST /api/assistant/sessions", func(w http.ResponseWriter, r *http.Request) {
		if a.Codex != nil {
			writeJSON(w, 410, map[string]string{"error": "Сервер использует локальный Codex. Обновите страницу; OpenAI Realtime больше не используется."})
			return
		}
		if !ready(w) {
			return
		}
		var input struct {
			WorkspaceID string `json:"workspace_id"`
			SDP         string `json:"sdp"`
		}
		if e := decodeAssistant(w, r, &input); e != nil {
			assistantError(w, e)
			return
		}
		if input.WorkspaceID == "" {
			input.WorkspaceID = "default"
		}
		reply, e := a.Assistant.Create(r.Context(), a.session, input.WorkspaceID, input.SDP)
		if e != nil {
			assistantError(w, e)
			return
		}
		writeJSON(w, 201, reply)
	})
	mux.HandleFunc("DELETE /api/assistant/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if e := a.Assistant.End(ctx, a.session, r.PathValue("id")); e != nil {
			assistantError(w, e)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /api/assistant/sessions/{id}/context", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		id, e := a.Assistant.SessionWorkspace(a.session, r.PathValue("id"))
		if e != nil {
			assistantError(w, e)
			return
		}
		instructions, e := a.Assistant.Instructions(r.Context(), id)
		if e != nil {
			assistantError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"instructions": instructions})
	})
	mux.HandleFunc("POST /api/assistant/sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		id, e := a.Assistant.SessionWorkspace(a.session, r.PathValue("id"))
		if e != nil {
			assistantError(w, e)
			return
		}
		var msg assistant.Message
		if e = decodeAssistant(w, r, &msg); e != nil {
			assistantError(w, e)
			return
		}
		if e = a.Assistant.Append(r.Context(), id, msg); e != nil {
			assistantError(w, e)
			return
		}
		delivery := "disabled"
		if msg.Role == "assistant" && a.Desktop != nil {
			conversation, err := a.Assistant.Conversation(r.Context(), id)
			if err == nil && conversation.Settings.Notifications {
				delivery, err = a.Desktop.Send(r.Context(), desktop.Notification{ID: "assistant:" + id + ":" + msg.ID, Title: "Forma AI", Subtitle: "Ответ ассистента", Message: msg.Text})
				if err != nil {
					writeJSON(w, 200, map[string]any{"ok": true, "notification_status": "error", "notification_error": err.Error()})
					return
				}
			}
		}
		writeJSON(w, 200, map[string]any{"ok": true, "notification_status": delivery})
	})
	mux.HandleFunc("POST /api/assistant/sessions/{id}/tools", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var call assistant.ToolCall
		if e := decodeAssistant(w, r, &call); e != nil {
			assistantError(w, e)
			return
		}
		reply, e := a.Assistant.Call(r.Context(), a.session, r.PathValue("id"), call)
		if e != nil {
			assistantError(w, e)
			return
		}
		writeJSON(w, 200, reply)
	})
}
