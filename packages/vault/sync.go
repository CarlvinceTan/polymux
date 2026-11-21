package vault

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
)

// SyncStatus describes the optional sync location. State is "off", "synced", "pending"
// (changes not yet written there), "unavailable" (the folder or file can't be reached) or
// "locked" (the file there doesn't open with the vault password).
type SyncStatus struct {
	Location string `json:"location"`
	State    string `json:"state"`
	SyncedAt int64  `json:"syncedAt"`
}

var (
	ErrSyncUnavailable = errors.New("the sync location is unavailable")
	ErrWeakPassword    = errors.New("a new synced vault needs a password of at least 8 characters")
)

type syncState struct {
	external stamp
	state    string
	syncedAt time.Time
}

// Sync reports the sync location after bringing in any changes from it.
func (s *Store) Sync(ctx context.Context) (SyncStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return SyncStatus{}, err
	}
	return s.syncStatus(), nil
}

func (s *Store) syncStatus() SyncStatus {
	if s.config.Sync == "" {
		return SyncStatus{State: "off"}
	}
	status := SyncStatus{Location: s.config.Sync, State: s.sync.state}
	if !s.sync.syncedAt.IsZero() {
		status.SyncedAt = s.sync.syncedAt.UnixMilli()
	}
	return status
}

// SetSync keeps the vault in step with a KeePass file at location: a folder (using
// vault.kdbx in it) or a .kdbx path, for a folder that another service syncs. An existing
// file must open with password, or the current vault password when password is empty, and
// is merged in. A new file needs a password. Either way that password becomes the vault's,
// so the synced file opens on other devices. An empty location turns sync off.
func (s *Store) SetSync(ctx context.Context, location, password string) (SyncStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return SyncStatus{}, err
	}
	if location == "" {
		next := s.config
		next.Sync = ""
		if err := s.saveConfig(next); err != nil {
			return SyncStatus{}, err
		}
		s.sync = syncState{}
		return s.syncStatus(), nil
	}
	if !filepath.IsAbs(location) {
		return SyncStatus{}, ErrInvalid
	}
	path := filepath.Clean(location)
	if !strings.EqualFold(filepath.Ext(path), ".kdbx") {
		path = filepath.Join(path, FileName)
	}
	if path == s.path() {
		return SyncStatus{}, ErrInvalid
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
		return SyncStatus{}, ErrSyncUnavailable
	}
	if _, exists := stampOf(path); exists {
		if password == "" {
			password = s.password
		}
		if _, err := readDatabase(path, password); err != nil {
			return SyncStatus{}, ErrKDBX
		}
	} else if len(password) < 8 {
		return SyncStatus{}, ErrWeakPassword
	}
	if err := s.rekey(password); err != nil {
		return SyncStatus{}, err
	}
	next := s.config
	next.Sync = path
	if err := s.saveConfig(next); err != nil {
		return SyncStatus{}, err
	}
	s.sync = syncState{}
	s.exchange()
	status := s.syncStatus()
	switch status.State {
	case "locked":
		return status, ErrKDBX
	case "unavailable", "pending":
		return status, ErrSyncUnavailable
	}
	return status, nil
}

// rekey makes password the vault's, keeping the held copy and the file in step.
func (s *Store) rekey(password string) error {
	if password == s.password {
		return nil
	}
	old := s.password
	if err := s.keys.Save([]byte(password)); err != nil {
		return ErrUnavailable
	}
	s.db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	s.password = password
	if err := s.writeLocal(); err != nil {
		_ = s.keys.Save([]byte(old))
		s.password = old
		return err
	}
	return nil
}

// pull brings in the sync location's changes when its file changed or a push is pending.
func (s *Store) pull() {
	if s.config.Sync == "" {
		return
	}
	if current, ok := stampOf(s.config.Sync); ok && current == s.sync.external && s.sync.state == "synced" {
		return
	}
	s.exchange()
}

// push sends local changes to the sync location; failures leave them pending.
func (s *Store) push() {
	if s.config.Sync != "" {
		s.exchange()
	}
}

// exchange merges the synced file and its conflict copies into the embedded vault, then
// writes the result back there when it lacks anything. The other service does the syncing.
func (s *Store) exchange() {
	path := s.config.Sync
	for range 3 {
		if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
			s.sync.state = "unavailable"
			return
		}
		before, exists := stampOf(path)
		var external *gokeepasslib.Database
		if exists {
			db, err := readDatabase(path, s.password)
			if errors.Is(err, ErrKDBX) {
				s.sync.state = "locked"
				return
			}
			if err != nil {
				s.sync.state = "unavailable"
				return
			}
			external = db
		} else if placeholder(path) {
			download(path)
			s.sync.state = "unavailable"
			return
		}
		copies := []string{}
		changed := false
		for _, copy := range conflictCopies(path) {
			if db, err := readDatabase(copy, s.password); err == nil {
				changed = mergeInto(s.db, db) || changed
				copies = append(copies, copy)
			}
		}
		if external != nil {
			changed = mergeInto(s.db, external) || changed
		}
		if changed {
			if err := s.writeLocal(); err != nil {
				s.sync.state = "pending"
				return
			}
		}
		if external == nil || len(copies) > 0 || mergeInto(external, s.db) {
			if now, ok := stampOf(path); ok != exists || now != before {
				continue
			}
			data, err := encodeDatabase(s.db)
			if err == nil {
				err = writeAtomic(path, data)
			}
			if err != nil {
				s.sync.state = "pending"
				return
			}
			for _, copy := range copies {
				_ = os.Remove(copy)
			}
		}
		s.sync.external, _ = stampOf(path)
		s.sync.state, s.sync.syncedAt = "synced", s.now()
		return
	}
	s.sync.state = "pending"
}

// conflictCopies finds the copies sync services leave beside a file both devices changed:
// iCloud "vault 2.kdbx", Syncthing "vault.sync-conflict-….kdbx", Dropbox "vault (… conflicted
// copy …).kdbx" and Google Drive or OneDrive "vault (1).kdbx".
func conflictCopies(path string) []string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(base) + `( \d+| \(\d+\)|\.sync-conflict-[^/]+| \([^)]*conflicted copy[^)]*\))\.kdbx$`)
	names, _ := os.ReadDir(filepath.Dir(path))
	copies := []string{}
	for _, name := range names {
		if name.Type().IsRegular() && pattern.MatchString(name.Name()) {
			copies = append(copies, filepath.Join(filepath.Dir(path), name.Name()))
		}
	}
	return copies
}

// placeholder reports an iCloud file that is in the cloud but not on this Mac yet.
func placeholder(path string) bool {
	_, err := os.Stat(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".icloud"))
	return err == nil
}

func download(path string) {
	if runtime.GOOS == "darwin" {
		_ = exec.Command("/usr/bin/brctl", "download", path).Start()
	}
}
