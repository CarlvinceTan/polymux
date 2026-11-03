// Package network links two servers so one can move or merge into the other. Each keeps its own
// devices and enforces its own access.
package network

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/tunnel"
)

const announceEvery = time.Minute

// ErrRefused means a peer turned a request down, so trying it again won't help.
var ErrRefused = errors.New("the other server refused")

// errCut means the other side no longer accepts this server's credential.
var errCut = errors.New("server no longer linked")

// Invite is a pairing link's contents.
type Invite struct {
	URL    string `json:"url"`
	Token  string `json:"token"`
	Ticket string `json:"ticket,omitempty"`
}

// Identity names a server to one linking with it.
type Identity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Hello introduces a server and gives its new peer a way back.
type Hello struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Invite Invite `json:"invite"`
}

type Config struct {
	Store *device.Store
	ID    string
	Name  func() string
	// Invite turns a code from this server into a link to it.
	Invite func(ctx context.Context, code string) Invite
	// Tunnel is the folder that holds the sidecar for reaching peers on other networks.
	Tunnel string
	// Relocated is a marker left by a move, until every peer knows the new address.
	Relocated string
	Log       *slog.Logger
}

type Manager struct {
	config  Config
	client  *http.Client
	patient *http.Client
}

func New(config Config) *Manager {
	refuse := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Manager{config: config,
		client:  &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{}, CheckRedirect: refuse},
		patient: &http.Client{Transport: &http.Transport{}, CheckRedirect: refuse}}
}

// Run tells linked servers where this one went after a move, until they all know.
func (m *Manager) Run(ctx context.Context) {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if _, err := os.Stat(m.config.Relocated); err == nil && m.Announce(ctx) {
			_ = os.Remove(m.config.Relocated)
		}
		timer.Reset(announceEvery)
	}
}

// Link joins the server an invite leads to, and gives it a way back here.
func (m *Manager) Link(ctx context.Context, invite Invite) (device.Peer, error) {
	peer := device.Peer{URL: invite.URL, Ticket: invite.Ticket, Token: device.NewToken()}
	if err := m.redeem(ctx, invite, peer.Token); err != nil {
		return peer, err
	}
	var other Identity
	if err := m.call(ctx, peer, http.MethodGet, "state", nil, &other); err != nil {
		return peer, err
	}
	if other.ID == "" || other.ID == m.config.ID {
		return peer, errors.New("that's this server")
	}
	peer.ID, peer.Name = other.ID, other.Name
	code, err := m.config.Store.IssueGrant(ctx, device.Grant{Role: device.RolePeer, Client: peer.ID, Name: peer.Name})
	if err != nil {
		return peer, err
	}
	hello := Hello{ID: m.config.ID, Name: m.config.Name(), Invite: m.config.Invite(ctx, code)}
	if err := m.call(ctx, peer, http.MethodPost, "link", hello, nil); err != nil {
		return peer, err
	}
	return peer, m.config.Store.SavePeer(ctx, peer)
}

// Accept finishes a link another server started. self is the device row it called in on.
func (m *Manager) Accept(ctx context.Context, self string, hello Hello) error {
	peer := device.Peer{ID: hello.ID, Name: hello.Name, URL: hello.Invite.URL, Ticket: hello.Invite.Ticket, Token: device.NewToken()}
	if peer.ID == "" || peer.ID == m.config.ID {
		return errors.New("that's this server")
	}
	if err := m.redeem(ctx, hello.Invite, peer.Token); err != nil {
		return err
	}
	if err := m.config.Store.Claim(ctx, self, peer.ID); err != nil {
		return err
	}
	return m.config.Store.SavePeer(ctx, peer)
}

// Identity is who this server says it is.
func (m *Manager) Identity() Identity { return Identity{ID: m.config.ID, Name: m.config.Name()} }

func (m *Manager) redeem(ctx context.Context, invite Invite, token string) error {
	body := map[string]string{"code": invite.Token, "token": token}
	return m.reach(ctx, invite.URL, invite.Ticket, func(base string) error {
		return m.do(ctx, base, "", http.MethodPost, "/network/pair", body, nil)
	})
}

func (m *Manager) call(ctx context.Context, peer device.Peer, method, route string, in, out any) error {
	return m.reach(ctx, peer.URL, peer.Ticket, func(base string) error {
		return m.do(ctx, base, peer.Token, method, device.PeerPrefix+route, in, out)
	})
}

// reach tries the direct address, then the tunnel: a direct address can be this machine's own
// server, so a refusal there doesn't mean the peer refused.
func (m *Manager) reach(ctx context.Context, address, ticket string, attempt func(base string) error) error {
	err := errors.New("no secure route to that server")
	if trusted(address) {
		if err = attempt(address); err == nil || errors.Is(err, device.ErrRefused) {
			return err
		}
	}
	if ticket == "" {
		return err
	}
	base, stop, err := tunnel.Forward(ctx, m.config.Tunnel, ticket)
	if err != nil {
		return err
	}
	defer stop()
	return attempt(base)
}

func (m *Manager) do(ctx context.Context, base, token, method, path string, in, out any) error {
	return m.send(ctx, m.client, base, token, method, path, in, out)
}

func (m *Manager) send(ctx context.Context, client *http.Client, base, token, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(base, "/")+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Polymux-Client", "polymux-kit")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusForbidden {
		return device.ErrRefused
	}
	if response.StatusCode == http.StatusUnauthorized {
		return errCut
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return fmt.Errorf("%w: %s returned HTTP %d", ErrRefused, path, response.StatusCode)
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("%s returned HTTP %d", path, response.StatusCode)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// trusted matches the client's rule: plain HTTP would expose the credential beyond this machine.
func trusted(address string) bool {
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	if parsed.Hostname() == "localhost" {
		return true
	}
	ip := net.ParseIP(parsed.Hostname())
	return ip != nil && ip.IsLoopback()
}
