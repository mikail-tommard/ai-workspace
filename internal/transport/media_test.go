package transport

import (
	"aiworkspace/internal/workspace"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaUploadAuthenticatedServingAndIsolation(t *testing.T) {
	a, ts := testApp(t)
	root := t.TempDir()
	if e := a.Service.EnableMedia(filepath.Join(root, "media"), []string{root}); e != nil {
		t.Fatal(e)
	}
	var imageData bytes.Buffer
	if e := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 4, 3))); e != nil {
		t.Fatal(e)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, e := form.CreateFormFile("file", "чертёж.png")
	if e != nil {
		t.Fatal(e)
	}
	part.Write(imageData.Bytes())
	form.Close()
	headers := auth()
	headers["Content-Type"] = form.FormDataContentType()
	status, _, _ := request(t, ts, "POST", "/api/media/default", body.String(), nil)
	if status != 401 {
		t.Fatal(status)
	}
	headers["Origin"] = "http://evil.example"
	status, _, _ = request(t, ts, "POST", "/api/media/default", body.String(), headers)
	if status != 403 {
		t.Fatal(status)
	}
	delete(headers, "Origin")
	status, _, raw := request(t, ts, "POST", "/api/media/default", body.String(), headers)
	if status != 201 {
		t.Fatal(status, string(raw))
	}
	var result struct {
		Asset  workspace.MediaAsset `json:"asset"`
		Widget workspace.Widget     `json:"widget"`
	}
	if e = json.Unmarshal(raw, &result); e != nil {
		t.Fatal(e)
	}
	if result.Widget.Type != "image" || !strings.HasPrefix(result.Asset.URL, "/api/media/default/") {
		t.Fatal(result)
	}
	status, h, data := request(t, ts, "GET", result.Asset.URL, "", auth())
	if status != 200 || h.Get("Content-Type") != "image/png" || !bytes.Equal(data, imageData.Bytes()) {
		t.Fatal(status, h)
	}
	status, _, _ = request(t, ts, "GET", result.Asset.URL, "", nil)
	if status != 401 {
		t.Fatal(status)
	}
	rawArgs, _ := json.Marshal(map[string]any{"name": "Other media workspace"})
	other, e := a.Service.Execute(context.Background(), "workspace_create", rawArgs)
	if e != nil {
		t.Fatal(e)
	}
	ws := other["workspace_id"].(string)
	status, _, _ = request(t, ts, "GET", "/api/media/"+ws+"/"+result.Asset.ID, "", auth())
	if status != 404 {
		t.Fatal("cross-workspace file leak", status)
	}
	status, _, _ = request(t, ts, "GET", "/api/media/default/../../mcp-token", "", auth())
	if status == 200 {
		t.Fatal("traversal accepted")
	}
	// Forged extension must never make HTML/SVG an executable uploaded image.
	var bad bytes.Buffer
	f := multipart.NewWriter(&bad)
	p, _ := f.CreateFormFile("file", "evil.png")
	p.Write([]byte("<script>alert(1)</script>"))
	f.Close()
	headers["Content-Type"] = f.FormDataContentType()
	status, _, _ = request(t, ts, "POST", "/api/media/default", bad.String(), headers)
	if status != 400 {
		t.Fatal(status)
	}
}
