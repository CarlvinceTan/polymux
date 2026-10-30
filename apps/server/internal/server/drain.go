package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
)

// admission covers whole jobs, including finalization and their asynchronous descendants.
type admission struct {
	mu      sync.Mutex
	state   string
	id      string
	active  int
	changed chan struct{}
}

type drainStatus struct {
	Build    string `json:"buildSHA,omitempty"`
	Instance string `json:"instance,omitempty"`
	State    string `json:"state"`
	Active   int    `json:"active"`
	Blocked  int    `json:"blocked"`
}

func (s *Server) loadDrain() error {
	data, err := os.ReadFile(filepath.Join(s.config.DataDir, "maintenance"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) < 16 || len(data) > 128 {
		return errors.New("invalid maintenance identity")
	}
	s.admission.id, s.admission.state = string(data), "ready"
	return nil
}

func (a *admission) snapshot() drainStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status()
}

func (a *admission) status() drainStatus {
	state := a.state
	if state == "" {
		state = "running"
	}
	return drainStatus{State: state, Active: a.active}
}

func (a *admission) start() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != "" {
		return false
	}
	a.active++
	return true
}

func (a *admission) wait(ctx context.Context, stop <-chan struct{}) bool {
	for {
		a.mu.Lock()
		if ctx.Err() != nil {
			a.mu.Unlock()
			return false
		}
		if a.state == "" {
			a.active++
			a.mu.Unlock()
			return true
		}
		if a.changed == nil {
			a.changed = make(chan struct{})
		}
		changed := a.changed
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-stop:
			return false
		case <-changed:
		}
	}
}

// fork is called before an admitted parent returns, never for unrelated new work.
func (a *admission) fork() {
	a.mu.Lock()
	a.active++
	a.mu.Unlock()
}

func (a *admission) done() {
	a.mu.Lock()
	a.active--
	a.mu.Unlock()
}

func (s *Server) drain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		status := s.admission.snapshot()
		status.Build, status.Instance = s.config.BuildSHA, s.instance
		write(w, 200, status)
		return
	}
	// Paired clients can observe maintenance; only the server owner can control it.
	self := device.Self(r.Context())
	if s.config.Devices == nil || self == "" || self != s.config.Devices.Host(r.Context(), s.config.DataDir) {
		fail(w, 403, "owner_required", "Server updates require the owner credential.")
		return
	}
	var input struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.ID) < 16 || len(input.ID) > 128 {
		fail(w, 400, "invalid_request", "Supply a bounded update identity.")
		return
	}
	a := &s.admission
	a.mu.Lock()
	defer a.mu.Unlock()
	if input.Action == "begin" && a.state == "" {
		if err := os.WriteFile(filepath.Join(s.config.DataDir, "maintenance"), []byte(input.ID), 0600); err != nil {
			s.respond(w, nil, err)
			return
		}
		a.id, a.state = input.ID, "draining"
	}
	if a.id != input.ID && !(input.Action == "cancel" && a.state == "") {
		fail(w, 409, "update_changed", "The update owner or server process changed. No restart is safe.")
		return
	}
	status := a.status()
	switch input.Action {
	case "begin":
	case "seal":
		// A failed finalization must block activation even after its goroutine returned.
		err := s.Store.DB.QueryRowContext(r.Context(), `SELECT
			(SELECT count(*) FROM jobs WHERE state='running') +
			(SELECT count(*) FROM runs WHERE status IN ('running','resuming','waiting_input'))`).Scan(&status.Blocked)
		if err != nil {
			s.respond(w, nil, err)
			return
		}
		if a.active == 0 && status.Blocked == 0 {
			a.state = "ready"
			status.State = "ready"
		}
	case "cancel":
		if a.state == "" {
			break
		}
		if err := os.Remove(filepath.Join(s.config.DataDir, "maintenance")); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.respond(w, nil, err)
			return
		}
		a.id, a.state = "", ""
		if a.changed != nil {
			close(a.changed)
			a.changed = nil
		}
		status = a.status()
	default:
		fail(w, 400, "invalid_request", "Choose begin, seal, or cancel.")
		return
	}
	status.Build, status.Instance = s.config.BuildSHA, s.instance
	write(w, 200, status)
}

// Writes remain available while draining, especially messages and approval answers.
// Once sealed, none can race the installer's restart decision.
func (s *Server) admitRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/maintenance" {
		return true
	}
	execution := executionRequest(r)
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && !execution {
		return true
	}
	a := &s.admission
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == "ready" || execution && a.state != "" {
		w.Header().Set("Retry-After", "2")
		fail(w, 503, "updating", "The server is updating. Try again shortly.")
		return false
	}
	a.active++
	return true
}

func (s *Server) requestDone(r *http.Request) {
	if r.URL.Path == "/maintenance" {
		return
	}
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && !executionRequest(r) {
		return
	}
	s.admission.done()
}

func executionRequest(r *http.Request) bool {
	if r.URL.Path == "/speech/live" {
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/agents/") && strings.HasSuffix(r.URL.Path, "/mcp/status") {
		return true
	}
	if r.Method != http.MethodPost {
		return false
	}
	return r.URL.Path == "/providers/logins" || strings.HasSuffix(r.URL.Path, "/voice") ||
		strings.HasSuffix(r.URL.Path, "/voice/stream")
}
