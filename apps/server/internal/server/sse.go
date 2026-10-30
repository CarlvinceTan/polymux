package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ssePage bounds one read; a full page is followed by another read at once.
const ssePage = 500

// sseFrame is one streamed event: its cursor, its SSE event name and its JSON body.
type sseFrame struct {
	seq  int64
	name string
	data any
}

// serveEvents streams frames after the request's cursor until the client leaves, the server
// stops or gone reports true. changed is taken before each read, so no write between them is missed.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request, changed func() <-chan struct{},
	read func(after int64) ([]sseFrame, error), gone func() bool) {
	after, ok := cursor(w, r, "after")
	if !ok {
		return
	}
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n < 0 {
			fail(w, 400, "cursor", "Invalid Last-Event-ID.")
			return
		}
		after = max(after, n)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		wake := changed()
		frames, err := read(after)
		if err != nil {
			return
		}
		for _, frame := range frames {
			data, err := json.Marshal(frame.data)
			if err != nil {
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", frame.seq, frame.name, data); err != nil {
				return
			}
			after = frame.seq
		}
		if err := controller.Flush(); err != nil {
			return
		}
		_ = controller.SetWriteDeadline(time.Time{})
		if len(frames) == ssePage {
			continue
		}
		if len(frames) == 0 && gone != nil && gone() {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-wake:
		case <-heartbeat.C:
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
		}
	}
}
