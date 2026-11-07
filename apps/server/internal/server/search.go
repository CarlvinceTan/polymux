package server

import (
	"context"
	"net/http"
	"strings"
	"time"
)

func (s *Server) globalSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 2048 {
		fail(w, 400, "query", "Search query is too long.")
		return
	}
	kind := r.URL.Query().Get("type")
	if kind == "" {
		kind = "all"
	}
	switch kind {
	case "all", "conversations", "bots", "messages", "media", "files":
	default:
		fail(w, 400, "type", "Unknown search type.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	hits, semantic, err := s.config.Index.Global(ctx, query, kind, r.URL.Query().Get("thread"))
	mode := "lexical"
	if semantic {
		mode = "hybrid"
	}
	w.Header().Set("X-Polymux-Search", mode)
	s.respond(w, hits, err)
}
