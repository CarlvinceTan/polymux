package device

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// RolePeer marks a device that is another server: it reaches only the peer routes.
const RolePeer = "peer"

// PeerPrefix is the only API a peer may call, and only peers may call it.
const PeerPrefix = "/network/peer/"

// ErrRefused means this server won't admit that device.
var ErrRefused = errors.New("this server doesn't allow that device")

// Peer is another server on the network. Unlike a device's, its token is kept whole: it's this
// server's own credential there, in the same owner-only database.
type Peer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Ticket string `json:"ticket,omitempty"`
	Token  string `json:"-"`
}

// Member is a device or server's standing on the network; the newest record wins everywhere.
type Member struct {
	ID      string `json:"id"`
	Removed bool   `json:"removed"`
	At      int64  `json:"at"`
}

// Grant shapes the device an invitation creates.
type Grant struct {
	Role   string
	Client string
	Name   string
}

func (s *Store) addNetwork(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS peers(id TEXT PRIMARY KEY, name TEXT NOT NULL, url TEXT NOT NULL, ticket TEXT NOT NULL, token TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS merged(id TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS network_name(id INTEGER PRIMARY KEY CHECK(id=1), name TEXT NOT NULL);`)
	if err != nil {
		return err
	}
	for _, column := range [][2]string{{"devices", "client"}, {"invitations", "role"}, {"invitations", "client"}, {"invitations", "name"}} {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, column[0], column[1]).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE `+column[0]+` ADD COLUMN `+column[1]+` TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

// Claim records a device's stable ID the first time it reports one. Pairing again after a removal
// or restriction is a person's decision, so it clears them.
func (s *Store) Claim(ctx context.Context, id, client string) error {
	if client == "" || len(client) > 128 {
		return nil
	}
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET client=? WHERE id=? AND client='' AND revoked=0`, client, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE devices SET client='' WHERE client=? AND revoked=1`, client)
	return err
}

// Device returns one device's row, revoked or not.
func (s *Store) Device(ctx context.Context, id string) (Pairing, error) {
	pairs, err := s.List(ctx)
	if err != nil {
		return Pairing{}, err
	}
	for _, p := range pairs {
		if p.ID == id {
			return p, nil
		}
	}
	return Pairing{}, sql.ErrNoRows
}

// NetworkName is what this server's network is called; empty until a device names it.
func (s *Store) NetworkName(ctx context.Context) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM network_name WHERE id=1`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// NameNetwork saves the network's name. With first set, it only names a network that has none.
func (s *Store) NameNetwork(ctx context.Context, name string, first bool) error {
	query := `INSERT INTO network_name VALUES(1,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name`
	if first {
		query = `INSERT OR IGNORE INTO network_name VALUES(1,?)`
	}
	_, err := s.db.ExecContext(ctx, query, name)
	return err
}

// Allow lets a removed device back in with the credential it already holds.
func (s *Store) Allow(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET revoked=0 WHERE id=? AND revoked=1 AND client!=''`, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err == nil && n != 1 {
		return errors.New("device not found")
	}
	return err
}

func (s *Store) Peers(ctx context.Context) ([]Peer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,url,ticket,token FROM peers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	peers := []Peer{}
	for rows.Next() {
		var p Peer
		if err := rows.Scan(&p.ID, &p.Name, &p.URL, &p.Ticket, &p.Token); err != nil {
			return nil, err
		}
		peers = append(peers, p)
	}
	return peers, rows.Err()
}

func (s *Store) Peer(ctx context.Context, id string) (Peer, error) {
	var p Peer
	err := s.db.QueryRowContext(ctx, `SELECT id,name,url,ticket,token FROM peers WHERE id=?`, id).Scan(&p.ID, &p.Name, &p.URL, &p.Ticket, &p.Token)
	return p, err
}

func (s *Store) SavePeer(ctx context.Context, p Peer) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO peers VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,url=excluded.url,ticket=excluded.ticket,token=excluded.token`,
		p.ID, p.Name, p.URL, p.Ticket, p.Token)
	return err
}

// Absorb records that a server's data now lives here, and ends its link.
func (s *Store) Absorb(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO merged VALUES(?)`, id); err != nil {
		return err
	}
	return s.Cut(ctx, id)
}

// Merged lists servers this one has absorbed, so devices can stop looking for them.
func (s *Store) Merged(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM merged ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Relocate records where a peer went after it moved.
func (s *Store) Relocate(ctx context.Context, id, url, ticket string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE peers SET url=?,ticket=? WHERE id=?`, url, ticket, id)
	return err
}

// Own gives the app on a server's own machine a device of its own, ahead of a move that takes
// the local credential away with the data.
func (s *Store) Own(ctx context.Context, host Pairing) (string, error) {
	token := randomID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices(id,name,label,agent,capabilities,token_hash,paired,client) VALUES(?,?,?,?,?,?,1,?)`,
		randomID(), host.Name, host.Label, "local", "[]", hash(token), host.Client)
	return token, err
}

// Rehome settles a moved server on its new machine. The app here takes over the local credential,
// its old pairing with this server is redundant, and the server that used to be here is gone.
func (s *Store) Rehome(ctx context.Context, dir, owner, previous string) error {
	host := s.Host(ctx, dir)
	if owner != "" {
		pairs, err := s.List(ctx)
		if err != nil {
			return err
		}
		for _, p := range pairs {
			if p.Client != owner || p.ID == host {
				continue
			}
			if !p.Revoked {
				if err := s.Revoke(ctx, p.ID); err != nil {
					return err
				}
			}
			if _, err := s.db.ExecContext(ctx, `UPDATE devices SET client='' WHERE id=?`, p.ID); err != nil {
				return err
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE devices SET client=? WHERE id=?`, owner, host); err != nil {
		return err
	}
	return s.Cut(ctx, previous)
}

// Cut ends a device or server's access here.
func (s *Store) Cut(ctx context.Context, client string) error {
	pairs, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, p := range pairs {
		if p.Client != client {
			continue
		}
		if !p.Revoked {
			if err := s.Revoke(ctx, p.ID); err != nil {
				return err
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE devices SET client='' WHERE client=?`, client); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM peers WHERE id=?`, client)
	return err
}

// IsPeer reports whether a device is another server.
func (p Pairing) IsPeer() bool { return slices.Contains(p.Capabilities, RolePeer) }

// peerRoute keeps peers to their own API, so a linked server can't read this one's chats.
func peerRoute(p Pairing, path string) bool {
	return p.IsPeer() == strings.HasPrefix(path, PeerPrefix)
}

func capabilities(role string) (string, error) {
	if role == "" {
		return "[]", nil
	}
	data, err := json.Marshal([]string{role})
	return string(data), err
}

// NewToken makes a credential in the form pairing expects.
func NewToken() string { return randomID() }
