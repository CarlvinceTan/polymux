package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func settingsAPI(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(t.Context(), db, Config{DataDir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); db.Close() })
	return s
}

func settingsRequest(t *testing.T, api *Server, method, path, body string, status int) contract.Settings {
	t.Helper()
	w := httptest.NewRecorder()
	api.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	if w.Code != status {
		t.Fatalf("settings request status %d, want %d", w.Code, status)
	}
	if strings.Contains(w.Body.String(), "test-only-secret") {
		t.Fatal("API returned the provider key")
	}
	var v contract.Settings
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

func TestSharedSettingsAndKeyAPI(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	initial := settingsRequest(t, s, "GET", "/settings", "", 200)
	if initial.Appearance != "system" || initial.VoiceMode != "pipeline" || len(initial.SpeechKeys) != 0 {
		t.Fatal("invalid server defaults")
	}
	settingsRequest(t, s, "PATCH", "/settings", `{"appearance":"dark","liveModel":"server-live"}`, 200)
	settingsRequest(t, s, "PATCH", "/settings", `{"textModel":"server-text"}`, 200)
	settingsRequest(t, s, "PUT", "/settings/keys/gemini", `{"key":"test-only-secret"}`, 200)
	v := settingsRequest(t, s, "GET", "/settings", "", 200)
	if v.Appearance != "dark" || v.LiveModel != "server-live" || v.TextModel != "server-text" || len(v.SpeechKeys) != 1 {
		t.Fatal("another client cannot see shared settings")
	}
	settingsRequest(t, s, "PATCH", "/settings", `{"speechKeys":["gemini"]}`, 400)
	settingsRequest(t, s, "PATCH", "/settings", `{"liveModel":"../invalid"}`, 400)
	settingsRequest(t, s, "PUT", "/settings/keys/unknown", `{"key":"test-only-secret"}`, 400)
	settingsRequest(t, s, "PUT", "/settings/keys/gemini", `{"key":" "}`, 400)
	settingsRequest(t, s, "DELETE", "/settings/keys/gemini", "", 200)
	if len(settingsRequest(t, s, "GET", "/settings", "", 200).SpeechKeys) != 0 {
		t.Fatal("key removal not visible to other clients")
	}
}

func TestBrowserCannotChangeSettingsOrUseSpeech(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	for _, test := range []struct{ method, path, body string }{
		{"PATCH", "/settings", `{"appearance":"dark"}`},
		{"PUT", "/settings/keys/gemini", `{"key":"test-only-secret"}`},
		{"DELETE", "/settings/keys/gemini", ""},
		{"POST", "/speech/text", `{}`},
		{"GET", "/speech/live", ""},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			for _, header := range []string{"Origin", "Sec-Fetch-Site"} {
				r := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
				r.Header.Set(header, "http://untrusted.test")
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden {
					t.Fatal("browser-origin request accepted")
				}
			}
		})
	}
}
