package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

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
	after, ok := cursor(w, r, "after")
	if !ok {
		return
	}
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		value, err := strconv.ParseInt(last, 10, 64)
		if err != nil || value < 0 {
			fail(w, 400, "cursor", "Invalid Last-Event-ID.")
			return
		}
		after = max(after, value)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		events, err := s.Store.SidebarEvents(r.Context(), after, 500)
		if err != nil {
			return
		}
		for _, event := range events {
			data, err := json.Marshal(event)
			if err != nil {
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprintf(w, "id: %d\nevent: sidebar.updated\ndata: %s\n\n", event.Seq, data); err != nil {
				return
			}
			after = event.Seq
		}
		if err := controller.Flush(); err != nil {
			return
		}
		_ = controller.SetWriteDeadline(time.Time{})
		if len(events) == 500 {
			continue
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		case <-heartbeat.C:
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
		}
	}
}
