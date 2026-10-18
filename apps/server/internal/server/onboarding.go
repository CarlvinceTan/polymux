package server

import (
	"errors"
	"net/http"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

// focus posts the deterministic first-run card. The client schedules the call, as Rakazo's
// web client does, so a delay can be skipped once the user has started typing.
func (s *Server) focus(w http.ResponseWriter, r *http.Request) {
	t, ok := s.thread(w, r)
	if !ok {
		return
	}
	if t.ID != t.AgentID {
		fail(w, 400, "invalid_thread", "First-run questions belong to a direct conversation.")
		return
	}
	_, posted, err := s.Store.FocusCard(r.Context(), t.ID)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	write(w, http.StatusOK, contract.FocusStatus{Posted: posted})
}

// answerFocus records the chosen option and posts its acknowledgment.
func (s *Server) answerFocus(w http.ResponseWriter, r *http.Request) {
	t, ok := s.thread(w, r)
	if !ok {
		return
	}
	var request contract.FocusAnswer
	if !decode(w, r, &request) {
		return
	}
	if len(request.OptionID) > 64 {
		fail(w, 400, "invalid_option", "Choose one of the offered options.")
		return
	}
	if err := s.Store.AnswerFocus(r.Context(), t.ID, request.OptionID); err != nil {
		if errors.Is(err, store.ErrFocus) {
			fail(w, 409, "focus", "This question was already answered.")
			return
		}
		s.respond(w, nil, err)
		return
	}
	write(w, http.StatusOK, contract.FocusStatus{Posted: true})
}
