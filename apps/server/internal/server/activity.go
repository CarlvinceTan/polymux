package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func (s *Server) patchRoutine(w http.ResponseWriter, r *http.Request) {
	if !s.routineOwner(w, r) {
		return
	}
	var patch store.RoutinePatch
	if !decode(w, r, &patch) {
		return
	}
	routine, err := s.Store.PatchRoutine(r.Context(), r.PathValue("id"), r.PathValue("routineID"), patch)
	if errors.Is(err, store.ErrSchedule) {
		fail(w, http.StatusBadRequest, "schedule", "Choose an active or paused routine and a non-empty name.")
		return
	}
	s.respond(w, routine, err)
}

func (s *Server) peerHistory(w http.ResponseWriter, r *http.Request) {
	if !s.routineOwner(w, r) {
		return
	}
	if _, err := s.Store.Profile(r.Context(), r.PathValue("peerID")); err != nil {
		fail(w, 404, "not_found", "Agent not found.")
		return
	}
	before := int64(0)
	if value := r.URL.Query().Get("before"); value != "" {
		var err error
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before < 0 {
			fail(w, 400, "cursor", "Invalid history cursor.")
			return
		}
	}
	messages, err := s.Store.PeerHistory(r.Context(), r.PathValue("id"), r.PathValue("peerID"), before)
	s.respond(w, messages, err)
}

func (s *Server) activityObject(w http.ResponseWriter, r *http.Request) {
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil || seq < 1 {
		fail(w, 400, "cursor", "Invalid activity.")
		return
	}
	thread := r.PathValue("id")
	events, err := s.Store.Events(r.Context(), thread, seq-1, 1)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	if len(events) != 1 || events[0].Seq != seq || events[0].Payload == nil || events[0].Type != "activity" {
		fail(w, 404, "not_found", "Activity is no longer available.")
		return
	}
	event := events[0]
	payload := event.Payload
	text := ""
	switch {
	case strings.HasPrefix(payload.Kind, "memory."):
		if event.AgentID == nil {
			fail(w, 404, "not_found", "Memory is no longer available.")
			return
		}
		documents, err := s.memory.List(r.Context(), *event.AgentID, thread)
		if err != nil {
			s.respond(w, nil, err)
			return
		}
		for _, document := range documents {
			if document.Name == payload.ObjectID {
				text = document.Text
				break
			}
		}
	case strings.HasPrefix(payload.Kind, "skill."):
		if !filepath.IsAbs(payload.ObjectID) || filepath.Ext(payload.ObjectID) != ".md" {
			fail(w, 404, "not_found", "Skill is no longer available.")
			return
		}
		file, err := os.Open(payload.ObjectID)
		if err != nil {
			fail(w, 404, "not_found", "Skill is no longer available.")
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 65537))
		if err != nil {
			s.respond(w, nil, err)
			return
		}
		if len(data) > 65536 {
			fail(w, 413, "size", "Skill is too large to display.")
			return
		}
		text = string(data)
	case strings.HasPrefix(payload.Kind, "subagent."):
		messages, err := s.Store.ChildMessages(r.Context(), payload.ObjectID)
		if err != nil {
			s.respond(w, nil, err)
			return
		}
		parts := []string{}
		for _, message := range messages {
			parts = append(parts, store.Text(message))
		}
		text = strings.Join(parts, "\n\n")
	default:
		fail(w, 400, "kind", "This activity has no document.")
		return
	}
	write(w, 200, map[string]string{"title": payload.ObjectLabel, "text": text})
}
