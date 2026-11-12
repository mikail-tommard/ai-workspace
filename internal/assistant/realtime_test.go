package assistant

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestUnifiedWebRTCProtocol(t *testing.T) {
	client := NewRealtimeClient("server-secret", "")
	client.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer server-secret" || r.URL.String() != "https://api.openai.com/v1/realtime/calls" {
			t.Fatal("wrong authenticated endpoint")
		}
		_, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if e != nil {
			t.Fatal(e)
		}
		parts := multipart.NewReader(r.Body, params["boundary"])
		fields := map[string]string{}
		for {
			p, e := parts.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Fatal(e)
			}
			b, _ := io.ReadAll(p)
			fields[p.FormName()] = string(b)
		}
		if fields["sdp"] != "v=0\r\n" {
			t.Fatal(fields)
		}
		var config map[string]any
		if json.Unmarshal([]byte(fields["session"]), &config) != nil || config["model"] != DefaultModel {
			t.Fatal(fields)
		}
		return &http.Response{StatusCode: 201, Header: http.Header{"Location": []string{"/v1/realtime/calls/rtc_test"}}, Body: io.NopCloser(strings.NewReader("v=0\r\ns=answer\r\n"))}, nil
	})}
	sdp, id, e := client.Create(context.Background(), "v=0\r\n", map[string]any{"type": "realtime"})
	if e != nil || id != "rtc_test" || !strings.HasPrefix(sdp, "v=0") {
		t.Fatal(sdp, id, e)
	}
}
func TestUpstreamErrorsRedacted(t *testing.T) {
	for _, status := range []int{400, 401, 403, 429, 500} {
		client := NewRealtimeClient("secret", "")
		client.HTTP = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"secret credential diagnostics"}}`)), Header: http.Header{}}, nil
		})}
		_, _, e := client.Create(context.Background(), "v=0", map[string]any{})
		if e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatal(status, e)
		}
	}
}
func TestHangupUsesPrivateCallID(t *testing.T) {
	client := NewRealtimeClient("secret", "")
	client.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Path != "/v1/realtime/calls/rtc_test/hangup" {
			t.Fatal(r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	if e := client.Hangup(context.Background(), "rtc_test"); e != nil {
		t.Fatal(e)
	}
}
