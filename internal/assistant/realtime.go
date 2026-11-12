package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultModel = "gpt-realtime-2.1"

// RealtimeClient implements the GA unified WebRTC initialization API. Its key
// and provider call IDs remain backend-only; no ephemeral secret is exposed.
type RealtimeClient struct {
	Key   string
	Model string
	HTTP  *http.Client
}

func NewRealtimeClient(key, model string) *RealtimeClient {
	if model == "" {
		model = DefaultModel
	}
	return &RealtimeClient{Key: key, Model: model, HTTP: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *RealtimeClient) Configured() bool { return c.Key != "" }
func (c *RealtimeClient) Create(ctx context.Context, sdp string, config map[string]any) (string, string, error) {
	if !c.Configured() {
		return "", "", ErrUnavailable
	}
	config["model"] = c.Model
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if e := form.WriteField("sdp", sdp); e != nil {
		return "", "", e
	}
	b, e := json.Marshal(config)
	if e != nil {
		return "", "", e
	}
	if e = form.WriteField("session", string(b)); e != nil {
		return "", "", e
	}
	if e = form.Close(); e != nil {
		return "", "", e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/realtime/calls", &body)
	if e != nil {
		return "", "", e
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Accept", "application/sdp")
	res, e := c.HTTP.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "", "", fmt.Errorf("Не удалось подключиться к OpenAI Realtime. Проверьте сеть и настройки сервера.")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", "", providerError(res.StatusCode)
	}
	answer, e := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	if e != nil {
		return "", "", fmt.Errorf("Не удалось прочитать SDP OpenAI.")
	}
	if !strings.HasPrefix(strings.TrimSpace(string(answer)), "v=0") {
		return "", "", fmt.Errorf("OpenAI вернул некорректный SDP.")
	}
	location, e := url.Parse(res.Header.Get("Location"))
	if e != nil || location.RawQuery != "" || location.Fragment != "" || !strings.HasPrefix(location.Path, "/v1/realtime/calls/") {
		return "", "", fmt.Errorf("OpenAI не вернул идентификатор звонка.")
	}
	id := strings.TrimPrefix(location.Path, "/v1/realtime/calls/")
	if id == "" || strings.ContainsAny(id, "/\\") || len(id) > 200 {
		return "", "", fmt.Errorf("Некорректный идентификатор звонка.")
	}
	return string(answer), id, nil
}
func (c *RealtimeClient) Hangup(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	req, e := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/realtime/calls/"+url.PathEscape(id)+"/hangup", nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	res, e := c.HTTP.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("Не удалось завершить звонок OpenAI.")
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return providerError(res.StatusCode)
	}
	return nil
}
func providerError(status int) error {
	switch status {
	case 401, 403:
		return fmt.Errorf("OpenAI отклонил доступ: проверьте API-ключ и доступ к Realtime.")
	case 429:
		return fmt.Errorf("Лимит OpenAI исчерпан. Проверьте квоту и повторите позже.")
	default:
		return fmt.Errorf("OpenAI Realtime недоступен (HTTP %d). Повторите позже.", status)
	}
}
