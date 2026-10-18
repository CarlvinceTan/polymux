package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func (s *Server) sidebarRoutes() {
	s.mux.HandleFunc("GET /sidebar", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		state, err := s.Store.Sidebar(r.Context())
		s.respond(w, state, err)
	})
	s.mux.HandleFunc("GET /sidebar/events", s.sidebarEvents)
	s.mux.HandleFunc("GET /sidebar/stream", s.sidebarStream)
	s.mux.HandleFunc("GET /sections", func(w http.ResponseWriter, r *http.Request) {
		sections, err := s.Store.Sections(r.Context())
		s.respond(w, sections, err)
	})
	s.mux.HandleFunc("POST /sections", s.createSection)
	s.mux.HandleFunc("PATCH /sections/{id}", s.updateSection)
	s.mux.HandleFunc("DELETE /sections/{id}", s.deleteSection)
	s.mux.HandleFunc("PATCH /threads/{id}/placement", s.placeThread)
	s.mux.HandleFunc("POST /sidebar/reorder", s.reorderSidebar)
	s.mux.HandleFunc("POST /sidebar/import", s.importSidebar)
	s.mux.HandleFunc("GET /sidebar/sources/{source}", func(w http.ResponseWriter, r *http.Request) {
		source, err := s.Store.SidebarSource(r.Context(), r.PathValue("source"))
		if !s.sidebarError(w, err) {
			s.respond(w, source, err)
		}
	})
}

func (s *Server) sidebarError(w http.ResponseWriter, err error) bool {
	code, message, status := "", "", 400
	switch {
	case errors.Is(err, sql.ErrNoRows):
		code, message, status = "not_found", "The sidebar item could not be found.", 404
	case errors.Is(err, store.ErrSectionName):
		code, message = "invalid_name", "Enter a section name within forty characters."
	case errors.Is(err, store.ErrSectionTaken):
		code, message, status = "name_taken", "A section already uses this name.", 409
	case errors.Is(err, store.ErrSectionLimit):
		code, message, status = "section_limit", "You can create up to thirty sections.", 409
	case errors.Is(err, store.ErrSectionMissing):
		code, message, status = "section_not_found", "The section could not be found.", 404
	case errors.Is(err, store.ErrChatPinned):
		code, message = "chat_pinned", "Unpin the chat before moving it to a section."
	case errors.Is(err, store.ErrSidebarRate):
		code, message, status = "rate_limit", "Try again in a minute.", 429
	case errors.Is(err, store.ErrPlacement):
		code, message = "invalid_placement", "Choose an existing section and unpin the chat before moving it."
	case errors.Is(err, store.ErrAnchor):
		code, message = "bad_anchor", "Choose a different item in the same sidebar list."
	case errors.Is(err, store.ErrSidebarImport):
		code, message = "invalid_import", "Provide an explicit supported sidebar source manifest."
	case errors.Is(err, store.ErrPinLimit):
		code, message, status = "pin_limit", "You can pin up to twelve chats.", 409
	default:
		return false
	}
	fail(w, status, code, message)
	return true
}

func (s *Server) createSection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name    string  `json:"name"`
		AfterID *string `json:"afterID,omitempty"`
	}
	if !decode(w, r, &request) {
		return
	}
	section, created, err := s.Store.CreateSection(r.Context(), ID(), request.Name, request.AfterID)
	if s.sidebarError(w, err) {
		return
	}
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	write(w, status, section)
}

func (s *Server) updateSection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name      *string `json:"name,omitempty"`
		Collapsed *bool   `json:"collapsed,omitempty"`
	}
	if !decode(w, r, &request) {
		return
	}
	if request.Name == nil && request.Collapsed == nil {
		fail(w, 400, "invalid_request", "Choose a section field to update.")
		return
	}
	section, err := s.Store.UpdateSection(r.Context(), r.PathValue("id"), request.Name, request.Collapsed)
	if !s.sidebarError(w, err) {
		s.respond(w, section, err)
	}
}

func (s *Server) deleteSection(w http.ResponseWriter, r *http.Request) {
	ids, err := s.Store.DeleteSection(r.Context(), r.PathValue("id"))
	if !s.sidebarError(w, err) {
		s.respond(w, map[string]any{"deleted": r.PathValue("id"), "returnedChatIDs": ids}, err)
	}
}

func (s *Server) placeThread(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Pinned    *bool           `json:"pinned,omitempty"`
		SectionID json.RawMessage `json:"sectionID,omitempty"`
		BeforeID  *string         `json:"beforeID,omitempty"`
		AfterID   *string         `json:"afterID,omitempty"`
	}
	if !decode(w, r, &request) {
		return
	}
	if request.Pinned == nil && request.SectionID == nil && request.BeforeID == nil && request.AfterID == nil {
		fail(w, 400, "invalid_request", "Choose a chat placement to update.")
		return
	}
	placement := store.Placement{Pinned: request.Pinned, SectionSet: request.SectionID != nil, BeforeID: request.BeforeID, AfterID: request.AfterID}
	if request.SectionID != nil {
		if err := json.Unmarshal(request.SectionID, &placement.SectionID); err != nil {
			fail(w, 400, "invalid_placement", "Choose a section or the main list.")
			return
		}
	}
	err := s.Store.PlaceThread(r.Context(), r.PathValue("id"), placement)
	if s.sidebarError(w, err) {
		return
	}
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	thread, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	s.respond(w, thread, err)
}

func (s *Server) reorderSidebar(w http.ResponseWriter, r *http.Request) {
	var request store.SidebarOrder
	if !decode(w, r, &request) {
		return
	}
	order, err := s.Store.ReorderSidebar(r.Context(), request)
	if !s.sidebarError(w, err) {
		s.respond(w, map[string]any{"id": request.ID, "order": order}, err)
	}
}

func (s *Server) importSidebar(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Manifest store.SidebarImport `json:"manifest"`
		Apply    bool                `json:"apply"`
	}
	if !decode(w, r, &request) {
		return
	}
	report, err := s.Store.ImportSidebar(r.Context(), request.Manifest, request.Apply)
	if !s.sidebarError(w, err) {
		s.respond(w, report, err)
	}
}
