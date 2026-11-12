package transport

import (
	"aiworkspace/internal/desktop"
	"aiworkspace/internal/workspace"
	"net/http"
)

func (a *App) desktopRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/desktop/status", func(w http.ResponseWriter, r *http.Request) {
		if a.Desktop == nil {
			writeJSON(w, 200, desktop.Status{Supported: false, Engine: "macOS Notification Center", LastStatus: "unavailable", Note: "Системные уведомления не настроены в этом процессе."})
			return
		}
		writeJSON(w, 200, a.Desktop.Status())
	})
	mux.HandleFunc("POST /api/desktop/notifications", func(w http.ResponseWriter, r *http.Request) {
		if a.Desktop == nil {
			assistantError(w, desktop.ErrUnsupported)
			return
		}
		var n desktop.Notification
		if e := decodeAssistant(w, r, &n); e != nil {
			assistantError(w, e)
			return
		}
		n.Title = "Forma AI"
		n.Subtitle = "Ассистент рабочего пространства"
		status, e := a.Desktop.Send(r.Context(), n)
		if e != nil {
			assistantError(w, e)
			return
		}
		writeJSON(w, 202, map[string]string{"status": status})
	})
	mux.HandleFunc("POST /api/desktop/notifications/test", func(w http.ResponseWriter, r *http.Request) {
		if a.Desktop == nil {
			assistantError(w, desktop.ErrUnsupported)
			return
		}
		status, e := a.Desktop.Send(r.Context(), desktop.Notification{ID: "test:" + workspace.ID(), Title: "Forma AI", Subtitle: "Центр уведомлений macOS", Message: "Всё готово. Ответы ассистента будут приходить сюда — без озвучки."})
		if e != nil {
			assistantError(w, e)
			return
		}
		writeJSON(w, 202, map[string]string{"status": status})
	})
}
