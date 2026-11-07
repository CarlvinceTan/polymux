package server

import (
	"net/http"

	"github.com/CarlvinceTan/polymux/apps/server/internal/privacy"
)

// privacy reports the macOS permissions bots inherit from this server, for the app's Grant window.
func (s *Server) privacy(w http.ResponseWriter, r *http.Request) {
	status, err := privacy.Probe(r.Context())
	s.respond(w, status, err)
}
