package server

import "net/http"

func (s *Server) sidebarEvents(w http.ResponseWriter, r *http.Request) {
	after, ok := cursor(w, r, "after")
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	events, err := s.Store.SidebarEvents(r.Context(), after, limit(r))
	s.respond(w, events, err)
}

func (s *Server) sidebarStream(w http.ResponseWriter, r *http.Request) {
	s.serveEvents(w, r, s.Store.SidebarChanged, func(after int64) ([]sseFrame, error) {
		events, err := s.Store.SidebarEvents(r.Context(), after, ssePage)
		frames := make([]sseFrame, len(events))
		for i, event := range events {
			frames[i] = sseFrame{event.Seq, "sidebar.updated", event}
		}
		return frames, err
	}, nil)
}
