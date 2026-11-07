package network

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/tunnel"
)

const (
	// SealHeader names the sender's half of a move's key.
	SealHeader = "X-Polymux-Seal"
	// MergeHeader marks data that joins a server rather than replacing it.
	MergeHeader = "X-Polymux-Merge"
	// NoteFile is the MoveNote a move carries in the data folder.
	NoteFile = ".move-keys.json"
)

// MovedKey is a Keychain item a move carries. Its account depends on the data folder's path.
type MovedKey struct {
	Service string `json:"service"`
	Folder  string `json:"folder"`
	Suffix  string `json:"suffix"`
	Value   string `json:"value"`
}

// MoveNote travels with the data: where it came from, and the keys that aren't in the folder.
type MoveNote struct {
	From string     `json:"from"`
	Keys []MovedKey `json:"keys"`
}

// Key finds a carried key by its service and suffix.
func (n MoveNote) Key(service, suffix string) (string, bool) {
	for _, key := range n.Keys {
		if key.Service == service && key.Suffix == suffix {
			return key.Value, true
		}
	}
	return "", false
}

// CheckDevice is a device a target allows, so a merge can say who gains or loses access.
type CheckDevice struct {
	Client string `json:"client"`
	Name   string `json:"name"`
}

// Check is what a target tells a source before a move.
type Check struct {
	Build      string        `json:"build"`
	Empty      bool          `json:"empty"`
	Free       int64         `json:"free"`
	Key        string        `json:"key"`
	Supervised bool          `json:"supervised"`
	Devices    []CheckDevice `json:"devices"`
}

// PublicKey is the part of a move key that a Check carries.
func PublicKey(key *ecdh.PrivateKey) string { return publicKey(key) }

// Check asks a peer whether it can take this server's place.
func (m *Manager) Check(ctx context.Context, peer device.Peer) (Check, error) {
	var check Check
	return check, m.call(ctx, peer, http.MethodPost, "move/check", nil, &check)
}

// Send streams a sealed copy to a peer, which keeps it aside until Commit.
func (m *Manager) Send(ctx context.Context, peer device.Peer, target string, merge bool, write func(io.Writer) error) error {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	aead, err := sealCipher(key, target)
	if err != nil {
		return err
	}
	base, stop, err := m.route(ctx, peer)
	if err != nil {
		return err
	}
	defer stop()
	body, pipe := io.Pipe()
	go func() {
		seal := &sealer{out: pipe, aead: aead}
		err := write(seal)
		if err == nil {
			err = seal.Close()
		}
		pipe.CloseWithError(err)
	}()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+device.PeerPrefix+"move/receive", body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+peer.Token)
	request.Header.Set("X-Polymux-Client", "polymux-kit")
	request.Header.Set(SealHeader, publicKey(key))
	if merge {
		request.Header.Set(MergeHeader, "1")
	}
	// A transfer takes as long as it takes, so it only ends with its context.
	response, err := (&http.Client{Transport: &http.Transport{}}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
	if response.StatusCode >= 300 {
		return fmt.Errorf("the target refused the data: HTTP %d", response.StatusCode)
	}
	return nil
}

// Commit tells a peer to take over, or to take the data in. Past this, the source must never
// serve. A merge answers when it's done, which takes as long as the peer's running work.
func (m *Manager) Commit(ctx context.Context, peer device.Peer, merge bool) error {
	route := device.PeerPrefix + "move/commit"
	if merge {
		route += "?merge=1"
	}
	return m.reach(ctx, peer.URL, peer.Ticket, func(base string) error {
		return m.send(ctx, m.patient, base, peer.Token, http.MethodPost, route, nil, nil)
	})
}

// Abort tells a peer to throw its partial copy away.
func (m *Manager) Abort(ctx context.Context, peer device.Peer) error {
	return m.call(ctx, peer, http.MethodPost, "move/abort", nil, nil)
}

// route finds the way to a peer once, for a request that can't be sent twice.
func (m *Manager) route(ctx context.Context, peer device.Peer) (string, func(), error) {
	failure := errors.New("no secure route to that server")
	if trusted(peer.URL) {
		if failure = m.do(ctx, peer.URL, peer.Token, http.MethodGet, device.PeerPrefix+"state", nil, nil); failure == nil {
			return peer.URL, func() {}, nil
		}
	}
	if peer.Ticket == "" {
		return "", nil, failure
	}
	return tunnel.Forward(ctx, m.config.Tunnel, peer.Ticket)
}

// Announce tells every peer where this server is now, after it moved. It stays due until all have heard.
func (m *Manager) Announce(ctx context.Context) bool {
	peers, err := m.config.Store.Peers(ctx)
	if err != nil {
		return false
	}
	here := m.config.Invite(ctx, "")
	done := true
	for _, peer := range peers {
		if err := m.call(ctx, peer, http.MethodPost, "address", Invite{URL: here.URL, Ticket: here.Ticket}, nil); err != nil {
			m.config.Log.Debug("peer unavailable", "peer", peer.Name, "error", err)
			done = false
		}
	}
	return done
}
