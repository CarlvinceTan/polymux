package server

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/network"
	"github.com/CarlvinceTan/polymux/apps/server/internal/secrets"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	vaultstore "github.com/CarlvinceTan/polymux/packages/vault"
)

// mergeFolders are data folders whose children come along, keyed by random IDs. A name that's
// already here stays as it is.
var mergeFolders = []string{"agents", "attachments", "drafts", "groups", "media-jobs", "memory", "provider-keys", "skills", "worktrees"}

// mergeLeftFolders stay behind: they belong to one server, or aren't data.
var mergeLeftFolders = []string{"connections", "credentials", "devices", "plugins", "provider-device", "proxy", "sessions", "tunnel", "vault"}

// durableSkipped is all of durable.db. Its integer IDs come from one server's counter and are
// also written inside its records, so two can't be joined. A bot opens a new session instead.
var durableSkipped = []string{"conversations", "document_revisions", "documents", "durable_metadata", "durable_schema", "entries", "record_ids", "submissions", "tasks"}

// imports is what a merge must decrypt with the other server's keys. Reading it all up front means
// a missing key stops the merge before anything here changes.
type imports struct {
	items       []vaultstore.Item
	credentials map[string][]secrets.Credential
	providers   []settings.Provider
	keys        map[string]string
}

// memoryKeys is a vault key that travelled with a move, rather than one in this host's Keychain.
type memoryKeys struct{ key []byte }

func (k memoryKeys) Load() ([]byte, error) { return k.key, nil }
func (k memoryKeys) Save([]byte) error     { return nil }
func (k memoryKeys) Protection() string    { return "move" }

func openRead(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func (s *Server) readImports(ctx context.Context, folder string, note network.MoveNote) (imports, error) {
	found := imports{credentials: map[string][]secrets.Credential{}, keys: map[string]string{}}
	db, err := openRead(filepath.Join(folder, "polymux.db"))
	if err != nil {
		return found, err
	}
	defer db.Close()
	var keys vaultstore.Keys = hostKeys{Dir: filepath.Join(folder, "vault")}
	if value, ok := note.Key("com.polymux.vault", ""); ok {
		key, err := hex.DecodeString(value)
		if err != nil {
			return found, err
		}
		keys = memoryKeys{key: key}
	}
	var hasVault int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name='vault_meta'`).Scan(&hasVault); err != nil {
		return found, err
	}
	if hasVault > 0 {
		vault, err := vaultstore.Open(ctx, db, keys)
		if err != nil {
			return found, err
		}
		defer vault.Close()
		status, err := vault.Status(ctx)
		if err != nil {
			return found, err
		}
		if status.Created {
			list, err := vault.List(ctx)
			if err != nil {
				return found, err
			}
			for _, summary := range list {
				item, err := vault.Get(ctx, summary.ID)
				if err != nil {
					return found, err
				}
				item.Revision = 0
				found.items = append(found.items, item)
			}
		}
	}
	sealed, err := secrets.Open(filepath.Join(folder, "credentials"), db)
	if err != nil {
		return found, err
	}
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT owner FROM credentials`)
	if err != nil {
		return found, err
	}
	owners := []string{}
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			rows.Close()
			return found, err
		}
		owners = append(owners, owner)
	}
	rows.Close()
	for _, owner := range owners {
		found.credentials[owner] = sealed.All(owner)
	}
	theirs, err := settings.Open(filepath.Join(folder, "settings.json"))
	if err != nil {
		return found, err
	}
	for _, provider := range theirs.Providers() {
		found.providers = append(found.providers, provider)
		if value, ok := note.Key("com.polymux.provider", "/"+provider.ID); ok {
			key, err := hex.DecodeString(value)
			if err != nil {
				return found, err
			}
			found.keys[provider.ID] = string(key)
		}
	}
	return found, nil
}

// apply saves what was decrypted under this server's own keys. This server's version wins.
func (s *Server) apply(ctx context.Context, found imports) error {
	if len(found.items) > 0 {
		if status, err := s.vault.Status(ctx); err != nil {
			return err
		} else if !status.Created {
			if err := s.vault.Initialize(ctx); err != nil {
				return err
			}
		}
	}
	// One bad entry mustn't cost the rest, so each failure is kept and the others go on.
	var failed error
	for _, item := range found.items {
		if _, err := s.vault.Save(ctx, item); err != nil && !errors.Is(err, vaultstore.ErrConflict) {
			failed = errors.Join(failed, err)
		}
	}
	for owner, list := range found.credentials {
		for _, credential := range list {
			if _, err := s.secrets.Get(owner, credential.Name); err == nil {
				continue
			}
			failed = errors.Join(failed, s.secrets.Save(owner, credential))
		}
	}
	mine := s.settings.Providers()
	added := false
	for _, provider := range found.providers {
		if slices.ContainsFunc(mine, func(p settings.Provider) bool { return p.ID == provider.ID }) {
			continue
		}
		provider.Order = len(mine)
		mine = append(mine, provider)
		added = true
		if key, ok := found.keys[provider.ID]; ok {
			failed = errors.Join(failed, s.keys.Set(provider.ID, key))
		}
	}
	if added {
		failed = errors.Join(failed, s.settings.SetProviders(mine))
	}
	return failed
}

// adoptFolders moves files out of the copy. Their names are random, so a failed merge leaves
// strays and nothing else; the source still has its own.
func adoptFolders(folder, dir string) error {
	for _, name := range mergeFolders {
		entries, err := os.ReadDir(filepath.Join(folder, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(dir, name), 0o700); err != nil {
			return err
		}
		for _, entry := range entries {
			target := filepath.Join(dir, name, entry.Name())
			if _, err := os.Lstat(target); err == nil {
				continue
			}
			if err := os.Rename(filepath.Join(folder, name, entry.Name()), target); err != nil {
				return err
			}
		}
	}
	return nil
}

// absorb takes in a copy that has arrived. Nothing before the database commit can be seen by
// anyone, so a failure here leaves this server as it was and the source free to resume.
func (s *Server) absorb(ctx context.Context, peer device.Peer) error {
	folder := s.incoming()
	data, err := os.ReadFile(filepath.Join(folder, network.NoteFile))
	if err != nil {
		return err
	}
	var note network.MoveNote
	if err := json.Unmarshal(data, &note); err != nil {
		return err
	}
	found, err := s.readImports(ctx, folder, note)
	if err != nil {
		return err
	}
	id, err := s.seal(ctx, 1)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			s.unseal(id)
		}
	}()
	if err := adoptFolders(folder, s.config.DataDir); err != nil {
		return err
	}
	if _, err := s.Store.Merge(ctx, filepath.Join(folder, "polymux.db"), peer.Name, note.From, s.config.DataDir); err != nil {
		return err
	}
	committed = true
	// Sealing lasts through a restart, and this one has nothing left to wait for.
	if err := os.Remove(filepath.Join(s.config.DataDir, "maintenance")); err != nil {
		s.config.Log.Error("finish a merge", "error", err)
	}
	// The chats are in. What's left can't undo that, so a failure here is logged, not returned.
	if err := s.apply(ctx, found); err != nil {
		s.config.Log.Error("finish a merge", "error", err)
		_ = os.WriteFile(filepath.Join(s.config.DataDir, MoveIssue), []byte("Some keys from the merged server didn't arrive."), 0o600)
	}
	if err := s.config.Devices.Store().Absorb(ctx, peer.ID); err != nil {
		s.config.Log.Error("finish a merge", "error", err)
	}
	return os.RemoveAll(folder)
}

func (s *Server) peerMerge(w http.ResponseWriter, r *http.Request) {
	store := s.config.Devices.Store()
	self, err := store.Device(r.Context(), device.Self(r.Context()))
	if err != nil || self.Client == "" {
		fail(w, 409, "unknown_device", "This server isn't linked.")
		return
	}
	peer, err := store.Peer(r.Context(), self.Client)
	if err != nil {
		fail(w, 409, "unknown_device", "This server isn't linked.")
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, moveDrainLimit)
	defer cancel()
	if err := s.absorb(ctx, peer); err != nil {
		s.config.Log.Warn("merge", "error", err)
		_ = os.RemoveAll(s.incoming())
		fail(w, 422, "merge_failed", "Couldn't merge that server.")
		return
	}
	w.WriteHeader(204)
	// Bots and chats that arrived in the database only reach the running server through a restart.
	time.AfterFunc(200*time.Millisecond, s.config.Exit)
}
