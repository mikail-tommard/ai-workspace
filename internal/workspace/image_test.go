package workspace

import (
	"context"
	"testing"
)

func TestImageRegistrySecurity(t *testing.T) {
	r := NewRegistry()
	for _, address := range []string{"javascript:alert(1)", "file:///etc/passwd", "http://example.com/a.png", "https://user:password@example.com/a.png", "https://127.0.0.1/a.png", "https://10.0.0.1/a.png", "https://localhost/a.png"} {
		if e := r.Validate("image", map[string]any{"url": address, "alt": "Test"}); e == nil {
			t.Fatal("unsafe image URL accepted", address)
		}
	}
	if e := r.Validate("image", map[string]any{"url": "https://images.example.com/a.png", "alt": "Блок земли"}); e != nil {
		t.Fatal(e)
	}
}
func TestImageWidgetToolState(t *testing.T) {
	s, _, _ := newTest(t)
	_, e := s.Execute(context.Background(), "widget_create", []byte(`{"type":"image","title":"Блок земли","config":{"url":"https://images.example.com/dirt.png","alt":"Minecraft dirt"}}`))
	if e != nil {
		t.Fatal(e)
	}
	state, e := s.Snapshot("default")
	if e != nil || len(state.Widgets) != 1 || state.Widgets[0].Type != "image" {
		t.Fatal(state, e)
	}
}
