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

// connectGemini adds an enabled Gemini provider with this API key, which voice uses.
func connectGemini(t *testing.T, s *Server, key string) {
	t.Helper()
	s.keys = providerSecrets{"gemini": key}
	if err := s.settings.SetProviders([]contract.Provider{{ID: "gemini", Type: "gemini", Name: "Gemini", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
}

func settingsAPI(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(t.Context(), db, Config{DataDir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), VaultKeys: &vaultKeys{}})
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
	raw := httptest.NewRecorder()
	s.ServeHTTP(raw, httptest.NewRequest("GET", "/settings", nil))
	if strings.Contains(raw.Body.String(), `"speechKeys":null`) {
		t.Fatal("speechKeys must be a list, which clients require")
	}
	initial := settingsRequest(t, s, "GET", "/settings", "", 200)
	if initial.Appearance != "system" || initial.LiveProvider != "gemini" || len(initial.SpeechKeys) != 0 {
		t.Fatal("invalid server defaults")
	}
	settingsRequest(t, s, "PATCH", "/settings", `{"appearance":"dark","liveModel":"server-live"}`, 200)
	settingsRequest(t, s, "PATCH", "/settings", `{"voiceLanguage":"fr"}`, 200)
	connectGemini(t, s, "test-only-secret")
	v := settingsRequest(t, s, "GET", "/settings", "", 200)
	if v.Appearance != "dark" || v.LiveModel != "server-live" || v.VoiceLanguage != "fr" || len(v.SpeechKeys) != 1 {
		t.Fatal("another client cannot see shared settings")
	}
	settingsRequest(t, s, "PATCH", "/settings", `{"speechKeys":["gemini"]}`, 400)
	settingsRequest(t, s, "PATCH", "/settings", `{"liveModel":"../invalid"}`, 400)
	// Switching live provider starts from that provider's model and voice.
	if v := settingsRequest(t, s, "PATCH", "/settings", `{"liveProvider":"openai"}`, 200); v.LiveModel != "gpt-realtime" || v.LiveVoice != "marin" {
		t.Fatalf("switch kept the old provider's settings: %s %s", v.LiveModel, v.LiveVoice)
	}
	settingsRequest(t, s, "PATCH", "/settings", `{"liveVoice":"Kore"}`, 400)
}

func TestBrowserCannotChangeSettingsOrUseSpeech(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	for _, test := range []struct{ method, path, body string }{
		{"PATCH", "/settings", `{"appearance":"dark"}`},
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

func TestNewBotsStartOnTheSettingsDefault(t *testing.T) {
	t.Parallel()
	s := settingsAPI(t)
	settingsRequest(t, s, "PATCH", "/settings", `{"defaultReasoning":"extreme"}`, 400)
	for _, level := range []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"} {
		t.Run(level, func(t *testing.T) {
			settingsRequest(t, s, "PATCH", "/settings", `{"defaultReasoning":"`+level+`"}`, 200)
			if saved := settingsRequest(t, s, "GET", "/settings", "", 200); saved.DefaultReasoning != level {
				t.Fatalf("reasoning did not persist: %q", saved.DefaultReasoning)
			}
		})
	}

	settingsRequest(t, s, "PATCH", "/settings", `{"defaultProvider":"work"}`, 400)
	create := func(name string) contract.Agent {
		t.Helper()
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("POST", "/agents", strings.NewReader(`{"name":"`+name+`","role":"","prompt":""}`)))
		var agent contract.Agent
		if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &agent) != nil {
			t.Fatalf("create %s: %d %s", name, w.Code, w.Body.String())
		}
		return agent
	}
	settingsRequest(t, s, "PATCH", "/settings", `{"defaultProvider":"work","defaultModel":"openai/gpt-5"}`, 400)
	// With nothing connected there is no default, so a new bot is Not set.
	if bot := create("Early"); bot.Provider != "" || bot.Model != "" {
		t.Fatalf("fallback bot = %+v", bot)
	}
	if err := s.settings.SetProviders([]contract.Provider{{ID: "work", Type: "openai", Name: "Work", BaseURL: "https://example.com/v1", Enabled: true,
		Models: []contract.ProviderModel{{ID: "openai/gpt-5", Tags: []string{"text"}, Enabled: true}, {ID: "disabled", Enabled: false}}}}); err != nil {
		t.Fatal(err)
	}
	settingsRequest(t, s, "PATCH", "/settings", `{"defaultProvider":"work","defaultModel":"disabled"}`, 400)
	settingsRequest(t, s, "PATCH", "/settings", `{"defaultProvider":"work","defaultModel":"openai/gpt-5","defaultReasoning":"high"}`, 200)
	if bot := create("Later"); bot.Provider != "work" || bot.Model != "openai/gpt-5" || bot.Reasoning != "high" {
		t.Fatalf("default bot = %+v", bot)
	}
}
