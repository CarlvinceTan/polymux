package server

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
)

func nativeRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		fail(w, 403, "native_only", "Use the Polymux app to manage settings and speech.")
		return false
	}
	return true
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, s.voiceKeys(s.settings.Snapshot()))
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	var p contract.SettingsPatch
	if !decode(w, r, &p) {
		return
	}
	v, err := s.settings.Update(p)
	s.settingsResult(w, v, err)
}

func (s *Server) settingsResult(w http.ResponseWriter, v contract.Settings, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(err, settings.ErrInvalid) {
		fail(w, 400, "invalid_settings", "Choose valid settings and a supported voice provider.")
		return
	}
	s.respond(w, s.voiceKeys(v), err)
}

// voiceKeys marks the live voice providers the app can use: a connected provider of that type
// with an API key supplies it, so voice needs no key of its own. Keychain reads are cached
// briefly, since clients poll settings every few seconds.
func (s *Server) voiceKeys(v contract.Settings) contract.Settings {
	// Any change to providers or settings checks again at once.
	state := fmt.Sprint(v.Revision, s.settings.Providers())
	s.voiceMu.Lock()
	if state != s.voiceState || time.Since(s.voiceAt) > 30*time.Second {
		s.voiceState = state
		s.voiceFound = s.voiceFound[:0]
		for _, kind := range []string{"gemini", "openai"} {
			if s.speechKey(kind) != "" {
				s.voiceFound = append(s.voiceFound, kind)
			}
		}
		s.voiceAt = time.Now()
	}
	// Never null: clients decode speechKeys as a list.
	found := append([]string{}, s.voiceFound...)
	s.voiceMu.Unlock()
	v.SpeechKeys = found
	return v
}
