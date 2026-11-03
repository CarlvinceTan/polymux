package server

import (
	"net/http"
	"strconv"
)

const workLimit = 200

// recentWork lists recent runs and handoffs across every chat, for views of who is doing what.
func (s *Server) recentWork(w http.ResponseWriter, r *http.Request) {
	limit := workLimit
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			fail(w, 400, "limit", "Invalid limit.")
			return
		}
		limit = min(parsed, workLimit)
	}
	w.Header().Set("Cache-Control", "no-store")
	activity, err := s.Store.RecentWork(r.Context(), limit)
	activity.Build = s.config.BuildSHA
	s.respond(w, activity, err)
}
