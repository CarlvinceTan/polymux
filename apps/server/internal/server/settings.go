package server

import (
	"errors"
	"net/http"
	"strings"

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
	write(w, 200, s.settings.Snapshot())
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

func (s *Server) setSpeechKey(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	if r.PathValue("provider") != "gemini" {
		fail(w, 400, "invalid_provider", "Choose a supported provider.")
		return
	}
	key := ""
	if r.Method == http.MethodPut {
		var p contract.SpeechKey
		if !decode(w, r, &p) {
			return
		}
		key = strings.TrimSpace(p.Key)
		if key == "" {
			fail(w, 400, "invalid_key", "Enter an API key.")
			return
		}
	}
	v, err := s.settings.SetKey(key)
	s.settingsResult(w, v, err)
}

func (s *Server) settingsResult(w http.ResponseWriter, v contract.Settings, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(err, settings.ErrInvalid) {
		fail(w, 400, "invalid_settings", "Choose valid settings and a supported voice provider.")
		return
	}
	s.respond(w, v, err)
}
