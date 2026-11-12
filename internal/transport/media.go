package transport

import (
	"aiworkspace/internal/workspace"
	"mime"
	"net/http"
	"strings"
	"time"
)

func (a *App) mediaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/media/{workspace}", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, workspace.MaxMediaBytes+(64<<10))
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(time.Minute))
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			writeJSON(w, 415, map[string]string{"error": "Требуется multipart/form-data"})
			return
		}
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			writeJSON(w, 413, map[string]string{"error": "Не удалось прочитать загрузку. Максимум 25 МБ на файл."})
			return
		}
		defer r.MultipartForm.RemoveAll()
		if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
			writeJSON(w, 400, map[string]string{"error": "Передайте один файл в поле file"})
			return
		}
		for name, values := range r.MultipartForm.Value {
			if name != "attach" || len(values) != 1 || (values[0] != "true" && values[0] != "false") {
				writeJSON(w, 400, map[string]string{"error": "Некорректные параметры загрузки"})
				return
			}
		}
		file, header, e := r.FormFile("file")
		if e != nil {
			writeJSON(w, 400, map[string]string{"error": "Файл отсутствует"})
			return
		}
		defer file.Close()
		if header.Size > workspace.MaxMediaBytes {
			writeJSON(w, 413, map[string]string{"error": "Файл превышает 25 МБ"})
			return
		}
		result, e := a.Service.UploadMedia(r.Context(), r.PathValue("workspace"), header.Filename, file, r.FormValue("attach") != "false")
		if e != nil {
			a.reply(w, nil, e)
			return
		}
		writeJSON(w, 201, result)
	})
	mux.HandleFunc("GET /api/media/{workspace}/{id}", func(w http.ResponseWriter, r *http.Request) {
		asset, file, e := a.Service.OpenMedia(r.Context(), r.PathValue("workspace"), r.PathValue("id"))
		if e != nil {
			writeJSON(w, 404, map[string]string{"error": "Файл не найден"})
			return
		}
		defer file.Close()
		info, e := file.Stat()
		if e != nil {
			writeJSON(w, 404, map[string]string{"error": "Файл недоступен"})
			return
		}
		w.Header().Set("Content-Type", asset.MIME)
		disposition := "inline"
		if asset.MIME == "application/pdf" {
			disposition = "attachment"
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": asset.Name}))
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		http.ServeContent(w, r, asset.Name, info.ModTime(), file)
	})
}
