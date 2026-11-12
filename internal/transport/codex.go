package transport

import (
	"aiworkspace/internal/codexbridge"
	"errors"
	"net/http"
)

func (a *App) codexRoutes(mux *http.ServeMux) {
	ready := func(w http.ResponseWriter) bool {
		if a.Codex == nil {
			writeJSON(w, 503, map[string]string{"error": "Локальный Codex не настроен. Перезапустите обновлённый сервер."})
			return false
		}
		return true
	}
	mux.HandleFunc("POST /api/assistant/codex/workspaces/{id}/turns", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var input struct {
			ID   string `json:"request_id"`
			Text string `json:"text"`
		}
		if e := decodeAssistant(w, r, &input); e != nil {
			assistantError(w, e)
			return
		}
		state, e := a.Codex.Start(r.Context(), r.PathValue("id"), input.ID, input.Text)
		if e != nil {
			status := 400
			if errors.Is(e, codexbridge.ErrBusy) {
				status = 409
			}
			writeJSON(w, status, map[string]string{"error": e.Error()})
			return
		}
		writeJSON(w, 202, state)
	})
	mux.HandleFunc("GET /api/assistant/codex/workspaces/{id}/state", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		if _, e := a.Service.Snapshot(r.PathValue("id")); e != nil {
			assistantError(w, e)
			return
		}
		writeJSON(w, 200, a.Codex.Snapshot(r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/assistant/codex/workspaces/{id}/decision", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var input struct {
			ID      string `json:"call_id"`
			Approve bool   `json:"approve"`
		}
		if e := decodeAssistant(w, r, &input); e != nil {
			assistantError(w, e)
			return
		}
		if e := a.Codex.Decide(r.PathValue("id"), input.ID, input.Approve); e != nil {
			assistantError(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/assistant/codex/workspaces/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		a.Codex.Cancel(r.PathValue("id"))
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
}
