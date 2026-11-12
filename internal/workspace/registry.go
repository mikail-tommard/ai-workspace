package workspace

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

type Registry map[string]*Schema

func NewRegistry() Registry {
	text := Obj(map[string]*Schema{"content": Str("Plain text or safe Markdown", 100000)}, "content")
	return Registry{
		"markdown":     text,
		"gallery":      Obj(map[string]*Schema{"images": Array(Obj(map[string]*Schema{"url": Str("Managed asset or public HTTPS image URL", 2048), "alt": Str("Image description", 500), "caption": Str("Caption", 1000)}, "url", "alt"), 1, 20)}, "images"),
		"video":        Obj(map[string]*Schema{"url": Str("Managed asset or public HTTPS video URL", 2048), "caption": Str("Caption", 1000)}, "url"),
		"audio":        Obj(map[string]*Schema{"url": Str("Managed asset or public HTTPS audio URL", 2048), "caption": Str("Caption", 1000)}, "url"),
		"file":         Obj(map[string]*Schema{"url": Str("Managed file or public HTTPS download URL", 2048), "filename": Str("Display filename", 200), "description": Str("Description", 1000)}, "url", "filename"),
		"checklist":    Obj(map[string]*Schema{"items": Array(Obj(map[string]*Schema{"id": Str("Unique item ID", 100), "text": Str("Task text", 500), "done": {Type: "boolean"}}, "id", "text", "done"), 0, 100)}, "items"),
		"metric":       Obj(map[string]*Schema{"value": Str("Display value, e.g. 42 or 12.5", 64), "unit": Str("Unit suffix", 40), "label": Str("Description", 200), "change": Str("Change text", 80), "trend": Enum("up", "down", "neutral")}, "value"),
		"progress":     Obj(map[string]*Schema{"value": Number(0, 100), "label": Str("Progress label", 200), "detail": Str("Details", 500)}, "value"),
		"image":        Obj(map[string]*Schema{"url": Str("Managed /api/media/... URL or public HTTPS image URL", 2048), "alt": Str("Accessible description", 500), "caption": Str("Caption or source credit", 1000)}, "url", "alt"),
		"code":         Obj(map[string]*Schema{"code": Str("Source code; displayed, never executed", 100000), "language": Enum("go", "javascript", "typescript", "python", "json", "bash", "css", "html", "text")}, "code", "language"),
		"chart":        Obj(map[string]*Schema{"kind": Enum("line", "bar", "pie", "scatter"), "points": Array(Obj(map[string]*Schema{"x": Number(-1e9, 1e9), "y": Number(-1e9, 1e9), "label": Str("Point label", 120)}, "x", "y"), 1, 500), "x_label": Str("Horizontal axis label", 100), "y_label": Str("Vertical axis label", 100)}, "kind", "points"),
		"table":        Obj(map[string]*Schema{"columns": Array(Str("Column header", 120), 1, 20), "rows": Array(Array(&Schema{Type: "string", MaxLength: 2000}, 1, 20), 0, 500)}, "columns", "rows"),
		"notification": Obj(map[string]*Schema{"message": Str("Message to user", 10000), "level": Enum("info", "success", "warning", "error")}, "message", "level"),
		"math_problem": Obj(map[string]*Schema{}), "quiz": Obj(map[string]*Schema{}),
	}
}
func (r Registry) Validate(kind string, config map[string]any) error {
	s, ok := r[kind]
	if !ok {
		return fmt.Errorf("unsupported widget type %q", kind)
	}
	if err := s.Validate(config, "config"); err != nil {
		return err
	}
	if kind == "image" || kind == "audio" || kind == "video" || kind == "file" {
		if err := validateMediaURL(config["url"].(string)); err != nil {
			return err
		}
	}
	if kind == "gallery" {
		for _, item := range config["images"].([]any) {
			if err := validateMediaURL(item.(map[string]any)["url"].(string)); err != nil {
				return err
			}
		}
	}
	if kind == "checklist" {
		ids := map[string]bool{}
		for _, item := range config["items"].([]any) {
			id := item.(map[string]any)["id"].(string)
			if ids[id] {
				return fmt.Errorf("checklist item IDs must be unique")
			}
			ids[id] = true
		}
	}

	if kind == "table" {
		cols := config["columns"].([]any)
		for _, row := range config["rows"].([]any) {
			if len(row.([]any)) != len(cols) {
				return fmt.Errorf("table rows must match column count")
			}
		}
	}
	if kind == "chart" && config["kind"] == "pie" {
		sum := 0.
		for _, v := range config["points"].([]any) {
			y := v.(map[string]any)["y"].(float64)
			if y < 0 {
				return fmt.Errorf("pie values must be nonnegative")
			}
			sum += y
		}
		if sum <= 0 {
			return fmt.Errorf("pie total must be positive")
		}
	}
	return nil
}

func validateMediaURL(address string) error {
	if mediaURLPattern.MatchString(address) {
		return nil
	}
	u, e := url.Parse(address)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("media URL must be a managed asset URL or public HTTPS URL without credentials")
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || (ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast())) {
		return fmt.Errorf("local network media URLs are not allowed")
	}
	return nil
}
