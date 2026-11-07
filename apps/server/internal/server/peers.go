package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/network"
)

func (s *Server) networkRoutes() {
	s.mux.HandleFunc("PATCH /network", s.nameNetwork)
	s.mux.HandleFunc("POST /network/peers", s.linkPeer)
	s.mux.HandleFunc("POST /network/devices/{id}/allow", s.allowDevice)
	s.mux.HandleFunc("GET "+device.PeerPrefix+"state", s.peerState)
	s.mux.HandleFunc("POST "+device.PeerPrefix+"link", s.peerLink)
}

// nameNetwork renames this server's network; with first set, it only names one that has no name.
func (s *Server) nameNetwork(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	if s.config.Devices == nil {
		fail(w, 404, "not_found", "Device pairing is unavailable.")
		return
	}
	var input struct {
		Name  string `json:"name"`
		First bool   `json:"first"`
	}
	if !decode(w, r, &input) {
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 64 {
		fail(w, 400, "invalid", "Choose a name of up to 64 characters.")
		return
	}
	if err := s.config.Devices.Store().NameNetwork(r.Context(), name, input.First); err != nil {
		fail(w, 500, "network", "The name could not be saved.")
		return
	}
	w.WriteHeader(204)
}

// peers builds the network layer once pairing is available.
func (s *Server) peers() *network.Manager {
	s.peerOnce.Do(func() {
		s.peerLayer = network.New(network.Config{
			Store: s.config.Devices.Store(),
			ID:    s.config.ServerID,
			Name:  func() string { name, _ := os.Hostname(); return name },
			Invite: func(ctx context.Context, code string) network.Invite {
				value := s.inviteAt(ctx, s.config.Address, code)
				return network.Invite{URL: value["url"], Token: value["token"], Ticket: value["ticket"]}
			},
			Tunnel:    filepath.Join(s.config.DataDir, "tunnel"),
			Relocated: filepath.Join(s.config.DataDir, ".relocated"),
			Log:       s.config.Log,
		})
	})
	return s.peerLayer
}

func (s *Server) runPeers(ctx context.Context) {
	defer s.workers.Done()
	s.peers().Run(ctx)
}

// empty reports whether retiring this server would lose nothing: no chats, bots, providers
// or vault entries. Anything it can't confirm counts as data.
func (s *Server) empty(ctx context.Context) bool {
	if len(s.settings.Providers()) > 0 {
		return false
	}
	var used bool
	if err := s.Store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages) OR EXISTS(SELECT 1 FROM agents)`).Scan(&used); err != nil || used {
		return false
	}
	if s.vault != nil {
		if items, err := s.vault.List(ctx); err != nil || len(items) > 0 {
			return false
		}
	}
	return true
}

func (s *Server) noPairing(w http.ResponseWriter) bool {
	if s.config.Devices != nil {
		return false
	}
	fail(w, 404, "not_found", "Device pairing is unavailable.")
	return true
}

func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(value); err != nil {
		fail(w, 400, "invalid", "Send a valid request.")
		return false
	}
	return true
}

// linkPeer joins this server with the one an invite leads to.
func (s *Server) linkPeer(w http.ResponseWriter, r *http.Request) {
	if s.noPairing(w) {
		return
	}
	var invite network.Invite
	if !readJSON(w, r, &invite) {
		return
	}
	peer, err := s.peers().Link(r.Context(), invite)
	if err != nil {
		s.config.Log.Warn("link server", "error", err)
		fail(w, 502, "link_failed", "Couldn't link that server.")
		return
	}
	write(w, 200, peer)
}

func (s *Server) allowDevice(w http.ResponseWriter, r *http.Request) {
	if s.noPairing(w) {
		return
	}
	if err := s.config.Devices.Store().Allow(r.Context(), r.PathValue("id")); err != nil {
		fail(w, 404, "not_found", "That device isn't restricted.")
		return
	}
	w.WriteHeader(204)
}

func (s *Server) peerState(w http.ResponseWriter, _ *http.Request) {
	write(w, 200, s.peers().Identity())
}

func (s *Server) peerLink(w http.ResponseWriter, r *http.Request) {
	var hello network.Hello
	if !readJSON(w, r, &hello) {
		return
	}
	if err := s.peers().Accept(r.Context(), device.Self(r.Context()), hello); err != nil {
		s.config.Log.Warn("accept server link", "error", err)
		fail(w, 502, "link_failed", "Couldn't link back.")
		return
	}
	w.WriteHeader(204)
}
