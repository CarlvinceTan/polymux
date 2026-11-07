package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/gorilla/websocket"
)

func TestSpeechFailuresDoNotExposeCredentials(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	for _, path := range []string{"/speech/live"} {
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
}

func TestSpeechLiveSanitizesBillingCloseReason(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	connectGemini(t, s, "test-only-secret")
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
	s.speechDial = func(ctx context.Context, _, _, _ string) (*websocket.Conn, error) {
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
	connectGemini(t, s, "test-only-secret")
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
	s.speechDial = func(ctx context.Context, _, _, key string) (*websocket.Conn, error) {
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
