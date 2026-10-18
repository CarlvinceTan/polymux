package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if strings.TrimSpace(runID) == "" {
		fail(w, 400, "invalid_request", "Missing run id.")
		return
	}
	if _, err := s.Store.Run(r.Context(), runID); errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Run not found.")
		return
	} else if err != nil {
		s.respond(w, nil, err)
		return
	}
	pending, err := s.Store.PendingApprovals(r.Context(), runID)
	s.respond(w, pending, err)
}

func (s *Server) approveApproval(w http.ResponseWriter, r *http.Request) {
	s.decideApproval(w, r, store.ApprovalApproved)
}

func (s *Server) denyApproval(w http.ResponseWriter, r *http.Request) {
	s.decideApproval(w, r, store.ApprovalDenied)
}

func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request, decision string) {
	if strings.TrimSpace(r.Header.Get("X-Polymux-Client")) == "" {
		fail(w, 401, "unauthorized", "Missing X-Polymux-Client header.")
		return
	}
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		fail(w, 400, "invalid_request", "Missing approval id.")
		return
	}
	a, err := s.Store.DecideApproval(r.Context(), id, decision)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Approval not found.")
		return
	}
	if err != nil {
		if errors.Is(err, store.ErrApproval) {
			if a.Status == store.ApprovalExpired || strings.Contains(err.Error(), "expired") {
				fail(w, 409, "expired", "This approval has expired.")
				return
			}
			fail(w, 409, "conflict", "This approval cannot be decided.")
			return
		}
		s.respond(w, nil, err)
		return
	}
	run, err := s.Store.Run(r.Context(), a.RunID)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	if run.Status == "waiting_input" && a.JobID != "" {
		if err := s.Store.RequeueJob(r.Context(), a.JobID); err != nil && !errors.Is(err, store.ErrApproval) {
			s.respond(w, nil, err)
			return
		}
	}
	write(w, 200, a)
}
