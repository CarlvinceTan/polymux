package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/gorilla/websocket"
)

type speechTransport func(*http.Request) (*http.Response, error)

func (f speechTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSpeechTextUsesServerKeyAndModel(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	settingsRequest(t, s, "PUT", "/settings/keys/gemini", `{"key":"test-only-secret"}`, 200)
	settingsRequest(t, s, "PATCH", "/settings", `{"textModel":"models/server-text"}`, 200)
	answer := `{"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]}`
	s.speechHTTP = &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != geminiHost || r.URL.Scheme != "https" || r.URL.Path != "/v1beta/models/server-text:generateContent" || r.URL.RawQuery != "" {
			t.Error("speech did not use the fixed provider and server model")
		}
		if r.Header.Get("x-goog-api-key") != "test-only-secret" {
			t.Error("server key missing")
		}
		data, _ := io.ReadAll(r.Body)
		if string(data) != `{"contents":[]}` {
			t.Error("speech body changed")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(answer))}, nil
	})}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/speech/text", strings.NewReader(`{"contents":[]}`)))
	if w.Code != 200 || w.Body.String() != answer || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("speech relay failed")
	}
}

func TestSpeechFailuresDoNotExposeCredentials(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	for _, path := range []string{"/speech/text", "/speech/live"} {
		method := "POST"
		if path == "/speech/live" {
			method = "GET"
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
		if w.Code != 409 {
			t.Fatal("missing key not actionable")
		}
	}
	settingsRequest(t, s, "PUT", "/settings/keys/gemini", `{"key":"test-only-secret"}`, 200)
	for _, test := range []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{"upstream error", 0, "", fmt.Errorf("authenticated URL test-only-secret")},
		{"provider error", 401, `{"error":"test-only-secret"}`, nil},
		{"depleted credits", 429, `{"error":"credits depleted for test-only-secret"}`, nil},
		{"invalid JSON", 200, "broken", nil},
		{"oversized", 200, strings.Repeat(" ", (8<<20)+1), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			s.speechHTTP = &http.Client{Transport: speechTransport(func(*http.Request) (*http.Response, error) {
				if test.err != nil {
					return nil, test.err
				}
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest("POST", "/speech/text", strings.NewReader(`{}`)))
			if w.Code != 502 || strings.Contains(w.Body.String(), "test-only-secret") {
				t.Fatal("provider error leaked credentials or succeeded")
			}
			if test.name == "depleted credits" && !strings.Contains(w.Body.String(), "speech_credits") {
				t.Fatal("billing failure lost its safe error category")
			}
		})
	}
}

func TestSpeechTextRejectsInvalidBodiesBeforeCallingProvider(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	if _, err := s.settings.SetKey("test-only-secret"); err != nil {
		t.Fatal(err)
	}
	s.speechHTTP = &http.Client{Transport: speechTransport(func(*http.Request) (*http.Response, error) {
		t.Error("invalid speech body reached provider")
		return nil, fmt.Errorf("unexpected provider call")
	})}
	for _, test := range []struct{ name, body string }{
		{"empty", ""}, {"null", "null"}, {"array", "[]"}, {"broken", "{"},
		{"oversized", `{"contents":"` + strings.Repeat("x", 1<<20) + `"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest("POST", "/speech/text", strings.NewReader(test.body)))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid body returned %d", w.Code)
			}
		})
	}
}

func TestSpeechLiveSanitizesBillingCloseReason(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	if _, err := s.settings.SetKey("test-only-secret"); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error("upstream upgrade failed")
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Error("setup missing")
			return
		}
		reason := "credits depleted for test-only-secret"
		if err := conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason), time.Now().Add(time.Second)); err != nil {
			t.Error("upstream close failed")
		}
	}))
	defer upstream.Close()
	s.speechDial = func(ctx context.Context, _ string) (*websocket.Conn, error) {
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(upstream.URL, "http"), nil)
		return conn, err
	}
	api := httptest.NewServer(s)
	defer api.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(api.URL, "http")+"/speech/live", nil)
	if err != nil {
		t.Fatal("client upgrade failed")
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"setup":{}}`)); err != nil {
		t.Fatal(err)
	}
	_, _, err = conn.ReadMessage()
	close, ok := err.(*websocket.CloseError)
	if !ok || close.Text != "Provider credits depleted." {
		t.Fatal("billing close was not sanitized and classified")
	}
}

func TestSpeechLiveRelaysAndClosesOnShutdown(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	if _, err := s.settings.SetKey("test-only-secret"); err != nil {
		t.Fatal(err)
	}
	model, voice := "server-live", "Kore"
	if _, err := s.settings.Update(contract.SettingsPatch{LiveModel: &model, LiveVoice: &voice}); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	observed := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error("upstream upgrade failed")
			return
		}
		defer conn.Close()
		defer close(closed)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var setup map[string]any
		if err := conn.ReadJSON(&setup); err != nil {
			t.Error("setup missing")
			return
		}
		observed <- setup
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"setupComplete":{}}`)); err != nil {
			t.Error("setup reply failed")
			return
		}
		kind, data, err := conn.ReadMessage()
		if err != nil {
			t.Error("audio frame missing")
			return
		}
		if err := conn.WriteMessage(kind, data); err != nil {
			t.Error("audio echo failed")
			return
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer upstream.Close()
	s.speechDial = func(ctx context.Context, key string) (*websocket.Conn, error) {
		if key != "test-only-secret" {
			t.Error("server key was not used")
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(upstream.URL, "http"), nil)
		return conn, err
	}
	api := httptest.NewServer(s)
	defer api.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(api.URL, "http")+"/speech/live", nil)
	if err != nil {
		t.Fatal("client upgrade failed")
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"setup":{"model":"models/stale-client","generationConfig":{"speechConfig":{}},"tools":[]}}`)); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil || string(data) != `{"setupComplete":{}}` {
		t.Fatal("setup reply not relayed")
	}
	message := <-observed
	setup := message["setup"].(map[string]any)
	speech := setup["generationConfig"].(map[string]any)["speechConfig"].(map[string]any)
	name := speech["voiceConfig"].(map[string]any)["prebuiltVoiceConfig"].(map[string]any)["voiceName"]
	if setup["model"] != "models/server-live" || name != "Kore" {
		t.Fatal("client overrode server speech configuration")
	}
	frame := `{"realtimeInput":{"audio":{"data":"AAAA"}}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatal(err)
	}
	_, data, err = conn.ReadMessage()
	if err != nil || string(data) != frame {
		t.Fatal("audio was not relayed")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("server shutdown left speech open")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream connection leaked")
	}
}
