package workspace

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPNG(t *testing.T, dir string) string {
	t.Helper()
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 3, 2))); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "photo.png")
	if e := os.WriteFile(path, b.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestLocalMediaImportAndIsolation(t *testing.T) {
	s, db, dbpath := newTest(t)
	ctx := context.Background()
	root := t.TempDir()
	path := testPNG(t, root)
	dir := filepath.Join(t.TempDir(), "media")
	if e := s.EnableMedia(dir, []string{root}); e != nil {
		t.Fatal(e)
	}
	result := execute(t, s, "media_import", map[string]any{"path": path, "title": "Чертёж"})
	w := result["widget"].(Widget)
	a := result["asset"].(MediaAsset)
	if w.Type != "image" || w.Config["url"] != a.URL || !strings.HasPrefix(a.URL, "/api/media/default/") {
		t.Fatal(result)
	}
	_, f, e := s.OpenMedia(ctx, "default", a.ID)
	if e != nil {
		t.Fatal(e)
	}
	data, _ := io.ReadAll(f)
	f.Close()
	original, _ := os.ReadFile(path)
	if !bytes.Equal(data, original) {
		t.Fatal("content changed")
	}
	ws := execute(t, s, "workspace_create", map[string]any{"name": "Other"})["workspace_id"].(string)
	if _, _, e = s.OpenMedia(ctx, ws, a.ID); e == nil {
		t.Fatal("asset leaked across workspace")
	}
	outside := testPNG(t, t.TempDir())
	if _, e = s.Execute(ctx, "media_import", []byte(`{"path":"`+outside+`"}`)); e == nil {
		t.Fatal("outside root accepted")
	}
	if e = os.Symlink(outside, filepath.Join(root, "escape.png")); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Execute(ctx, "media_import", []byte(`{"path":"`+filepath.Join(root, "escape.png")+`"}`)); e == nil {
		t.Fatal("symlink escape accepted")
	}
	again := execute(t, s, "media_import", map[string]any{"path": path, "attach": false})["asset"].(MediaAsset)
	if again.ID != a.ID {
		t.Fatal("dedup failed")
	}
	// The source can disappear; the copied asset and SQLite metadata survive restart.
	s.Close()
	db.Close()
	reopened, e := Open(dbpath)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	loaded, e := New(ctx, reopened)
	if e != nil {
		t.Fatal(e)
	}
	defer loaded.Close()
	if e = loaded.EnableMedia(dir, []string{root}); e != nil {
		t.Fatal(e)
	}
	_, f, e = loaded.OpenMedia(ctx, "default", a.ID)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
}
func TestMediaValidation(t *testing.T) {
	s, _, _ := newTest(t)
	if e := s.EnableMedia(t.TempDir(), nil); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	for _, data := range []string{"<script>alert(1)</script>", "<svg xmlns='http://www.w3.org/2000/svg'/>"} {
		if _, e := s.UploadMedia(ctx, "default", "fake.png", strings.NewReader(data), true); e == nil {
			t.Fatal("unsafe content accepted")
		}
	}
	for _, kind := range []string{"gallery", "video", "audio", "file"} {
		config := map[string]any{"url": "javascript:alert(1)"}
		if kind == "file" {
			config["filename"] = "x"
		}
		if kind == "gallery" {
			config = map[string]any{"images": []any{map[string]any{"url": "file:///etc/passwd", "alt": "x"}}}
		}
		if e := NewRegistry().Validate(kind, config); e == nil {
			t.Fatal(kind, "unsafe URL accepted")
		}
	}
}
func TestNewWidgetsValidationAndPersistence(t *testing.T) {
	s, db, path := newTest(t)
	configs := map[string]map[string]any{
		"gallery": {"images": []any{map[string]any{"url": "https://images.example.com/a.png", "alt": "Photo"}}}, "video": {"url": "https://media.example.com/a.mp4"}, "audio": {"url": "https://media.example.com/a.mp3"}, "file": {"url": "https://media.example.com/a.pdf", "filename": "a.pdf"}, "checklist": {"items": []any{map[string]any{"id": "first", "text": "First item", "done": false}}}, "metric": {"value": "42", "unit": "ед."}, "progress": {"value": 65.0, "label": "Проект"}}
	for kind, config := range configs {
		result := execute(t, s, "widget_create", map[string]any{"type": kind, "title": kind, "config": config})
		if kind == "checklist" {
			id := result["widget"].(Widget).ID
			execute(t, s, "widget_update", map[string]any{"widget_id": id, "config": map[string]any{"items": []any{map[string]any{"id": "first", "text": "First item", "done": true}}}})
		}
	}
	before, _ := s.Snapshot("default")
	s.Close()
	db.Close()
	reopened, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	loaded, e := New(context.Background(), reopened)
	if e != nil {
		t.Fatal(e)
	}
	defer loaded.Close()
	after, e := loaded.Snapshot("default")
	if e != nil || len(after.Widgets) != 7 || after.Version != before.Version {
		t.Fatal(after, e)
	}
	registry := NewRegistry()
	if e := registry.Validate("progress", map[string]any{"value": 101.0}); e == nil {
		t.Fatal("invalid progress accepted")
	}
	if e := registry.Validate("checklist", map[string]any{"items": []any{map[string]any{"id": "same", "text": "a", "done": false}, map[string]any{"id": "same", "text": "b", "done": true}}}); e == nil {
		t.Fatal("duplicate checklist IDs accepted")
	}
}
func TestMediaCancellationAndSafeDownloadName(t *testing.T) {
	s, _, _ := newTest(t)
	if e := s.EnableMedia(t.TempDir(), nil); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.UploadMedia(ctx, "default", "photo.png", strings.NewReader("not read"), true); e == nil {
		t.Fatal("cancelled upload accepted")
	}
	pdf := strings.NewReader("%PDF-1.4\n% test\n")
	result, e := s.UploadMedia(context.Background(), "default", "unsafe\r\nname.html", pdf, true)
	if e != nil {
		t.Fatal(e)
	}
	asset := result["asset"].(MediaAsset)
	if strings.ContainsAny(asset.Name, "\r\n") || !strings.HasSuffix(asset.Name, ".pdf") {
		t.Fatal("unsafe download filename", asset.Name)
	}
}
