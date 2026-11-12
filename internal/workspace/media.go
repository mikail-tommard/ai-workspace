package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const MaxMediaBytes int64 = 25 << 20
const maxWorkspaceMedia int64 = 250 << 20

var assetIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var mediaURLPattern = regexp.MustCompile(`^/api/media/[A-Za-z0-9_-]{1,100}/[a-f0-9]{64}$`)

type MediaAsset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	MIME string `json:"mime"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}
type MediaStore struct {
	mu    sync.Mutex
	dir   string
	roots []string
	db    *Store
}

func (s *Service) EnableMedia(dir string, roots []string) error {
	absolute, e := filepath.Abs(dir)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(absolute, 0700); e != nil {
		return e
	}
	s.media = &MediaStore{dir: absolute, roots: append([]string{}, roots...), db: s.store}
	return nil
}
func mediaName(name string) string {
	r := []rune(filepath.Base(name))
	if len(r) > 180 {
		r = r[:180]
	}
	name = strings.Map(func(ch rune) rune {
		if ch < 32 || ch == 127 {
			return -1
		}
		return ch
	}, string(r))
	if name == "" || name == "." {
		name = "file"
	}
	return name
}
func detectMedia(data []byte) (string, error) {
	kind := http.DetectContentType(data)
	switch kind {
	case "image/png", "image/jpeg", "image/gif":
		cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
		if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 16384 || cfg.Height > 16384 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
			return "", errors.New("Изображение повреждено или превышает 40 мегапикселей")
		}
	case "image/webp", "video/mp4", "video/webm", "audio/mpeg", "audio/ogg", "audio/aiff", "application/pdf":
	case "audio/wave":
		kind = "audio/wav"
	case "application/ogg":
		kind = "audio/ogg"
	default:
		return "", fmt.Errorf("Формат не поддерживается: %s. Используйте PNG, JPEG, GIF, WebP, MP4, WebM, MP3, WAV, OGG или PDF", kind)
	}
	return kind, nil
}
func (m *MediaStore) put(ctx context.Context, ws, name string, r io.Reader) (MediaAsset, error) {
	if e := ctx.Err(); e != nil {
		return MediaAsset{}, e
	}
	data, e := io.ReadAll(io.LimitReader(r, MaxMediaBytes+1))
	if e != nil {
		return MediaAsset{}, errors.New("Не удалось прочитать файл")
	}
	if int64(len(data)) > MaxMediaBytes {
		return MediaAsset{}, errors.New("Файл превышает 25 МБ")
	}
	if e = ctx.Err(); e != nil {
		return MediaAsset{}, e
	}
	kind, e := detectMedia(data)
	if kind == "video/mp4" && (strings.EqualFold(filepath.Ext(name), ".m4a") || strings.EqualFold(filepath.Ext(name), ".m4b")) {
		kind = "audio/mp4"
	}
	if e != nil {
		return MediaAsset{}, e
	}
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	safeName := mediaName(name)
	allowed := map[string][]string{"image/png": {".png"}, "image/jpeg": {".jpg", ".jpeg"}, "image/gif": {".gif"}, "image/webp": {".webp"}, "video/mp4": {".mp4", ".m4v"}, "video/webm": {".webm"}, "audio/mpeg": {".mp3"}, "audio/mp4": {".m4a", ".m4b"}, "audio/wav": {".wav"}, "audio/ogg": {".ogg", ".oga"}, "audio/aiff": {".aif", ".aiff"}, "application/pdf": {".pdf"}}[kind]
	validExt := false
	for _, ext := range allowed {
		if strings.EqualFold(filepath.Ext(safeName), ext) {
			validExt = true
		}
	}
	if !validExt && len(allowed) > 0 {
		safeName += allowed[0]
	}
	a := MediaAsset{ID: id, Name: safeName, MIME: kind, Size: int64(len(data)), URL: "/api/media/" + ws + "/" + id}
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, found, e := m.db.GetRecord(ctx, "media:"+ws+":"+id)
	if e != nil {
		return a, e
	}
	if found {
		if e = json.Unmarshal(raw, &a); e != nil {
			return a, e
		}
		return a, nil
	}
	var index []MediaAsset
	raw, found, e = m.db.GetRecord(ctx, "media-index:"+ws)
	if e != nil {
		return a, e
	}
	if found {
		if e = json.Unmarshal(raw, &index); e != nil {
			return a, e
		}
	}
	var total int64
	for _, old := range index {
		total += old.Size
	}
	if len(index) >= 200 || total+a.Size > maxWorkspaceMedia {
		return a, errors.New("Лимит файлов пространства: 200 файлов / 250 МБ")
	}
	temp, e := os.CreateTemp(m.dir, ".upload-*")
	if e != nil {
		return a, errors.New("Хранилище файлов недоступно")
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, e = temp.Write(data); e == nil {
		e = temp.Sync()
	}
	closeErr := temp.Close()
	if e != nil {
		return a, e
	}
	if closeErr != nil {
		return a, closeErr
	}
	// Content addressed: an existing file has exactly the same digest and is never overwritten.
	if e = os.Link(tempName, filepath.Join(m.dir, id)); e != nil && !errors.Is(e, os.ErrExist) {
		return a, errors.New("Не удалось сохранить файл")
	}
	encoded, _ := json.Marshal(a)
	if e = m.db.PutRecord(ctx, "media:"+ws+":"+id, encoded); e != nil {
		return a, e
	}
	index = append(index, a)
	encoded, _ = json.Marshal(index)
	if e = m.db.PutRecord(ctx, "media-index:"+ws, encoded); e != nil {
		return a, e
	}
	return a, nil
}
func (m *MediaStore) importFile(ctx context.Context, ws, path string) (MediaAsset, error) {
	absolute, e := filepath.Abs(path)
	if e != nil {
		return MediaAsset{}, errors.New("Некорректный путь")
	}
	for _, rootPath := range m.roots {
		rootAbs, err := filepath.Abs(rootPath)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(rootAbs, absolute)
		if err != nil || !filepath.IsLocal(relative) {
			continue
		}
		root, err := os.OpenRoot(rootAbs)
		if err != nil {
			continue
		}
		file, err := root.Open(relative)
		root.Close()
		if err != nil {
			return MediaAsset{}, errors.New("Файл недоступен или ссылка ведёт за пределы разрешённой папки")
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return MediaAsset{}, errors.New("Импортировать можно только обычный файл")
		}
		if info.Size() > MaxMediaBytes {
			return MediaAsset{}, errors.New("Файл превышает 25 МБ")
		}
		return m.put(ctx, ws, filepath.Base(path), file)
	}
	return MediaAsset{}, errors.New("Путь вне разрешённых папок импорта. Загрузите файл через панель или настройте WORKSPACE_MEDIA_IMPORT_ROOTS")
}
func (s *Service) mediaReady(ws string) error {
	if s.media == nil {
		return errors.New("Хранилище файлов не настроено в этом процессе")
	}
	_, e := s.Snapshot(ws)
	return e
}
func (s *Service) UploadMedia(ctx context.Context, ws, name string, r io.Reader, attach bool) (Result, error) {
	if e := s.mediaReady(ws); e != nil {
		return nil, e
	}
	a, e := s.media.put(ctx, ws, name, r)
	if e != nil {
		return nil, e
	}
	return s.attachMedia(ctx, ws, a, "", "", "", attach)
}
func (s *Service) importMedia(ctx context.Context, raw []byte) (Result, error) {
	if len(raw) > 1<<20 {
		return nil, errors.New("arguments exceed 1 MiB")
	}
	a, e := Decode(raw, s.tools["media_import"].InputSchema)
	if e != nil {
		return nil, e
	}
	ws, _ := a["workspace_id"].(string)
	if ws == "" {
		ws = "default"
	}
	if e = s.mediaReady(ws); e != nil {
		return nil, e
	}
	asset, e := s.media.importFile(ctx, ws, a["path"].(string))
	if e != nil {
		return nil, e
	}
	title, _ := a["title"].(string)
	alt, _ := a["alt"].(string)
	tab, _ := a["tab_id"].(string)
	attach := true
	if v, ok := a["attach"].(bool); ok {
		attach = v
	}
	return s.attachMedia(ctx, ws, asset, title, alt, tab, attach)
}
func (s *Service) attachMedia(ctx context.Context, ws string, a MediaAsset, title, alt, tab string, attach bool) (Result, error) {
	if !attach {
		return Result{"asset": a}, nil
	}
	if title == "" {
		title = a.Name
	}
	if alt == "" {
		alt = title
	}
	kind := "file"
	config := map[string]any{"url": a.URL, "filename": a.Name}
	switch {
	case strings.HasPrefix(a.MIME, "image/"):
		kind = "image"
		config = map[string]any{"url": a.URL, "alt": alt}
	case strings.HasPrefix(a.MIME, "video/"):
		kind = "video"
		config = map[string]any{"url": a.URL}
	case strings.HasPrefix(a.MIME, "audio/"):
		kind = "audio"
		config = map[string]any{"url": a.URL}
	}
	args := map[string]any{"workspace_id": ws, "type": kind, "title": title, "config": config}
	if tab != "" {
		args["tab_id"] = tab
	}
	raw, _ := json.Marshal(args)
	result, e := s.Execute(ctx, "widget_create", raw)
	if e != nil {
		return nil, e
	}
	result["asset"] = a
	return result, nil
}
func (s *Service) OpenMedia(ctx context.Context, ws, id string) (MediaAsset, *os.File, error) {
	if e := s.mediaReady(ws); e != nil {
		return MediaAsset{}, nil, e
	}
	if !assetIDPattern.MatchString(id) {
		return MediaAsset{}, nil, ErrNotFound
	}
	raw, found, e := s.store.GetRecord(ctx, "media:"+ws+":"+id)
	if e != nil || !found {
		if e == nil {
			e = ErrNotFound
		}
		return MediaAsset{}, nil, e
	}
	var a MediaAsset
	if e = json.Unmarshal(raw, &a); e != nil {
		return a, nil, e
	}
	root, e := os.OpenRoot(s.media.dir)
	if e != nil {
		return a, nil, ErrNotFound
	}
	defer root.Close()
	file, e := root.Open(id)
	if e != nil {
		return a, nil, ErrNotFound
	}
	return a, file, nil
}
