package vault

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

var (
	ErrConflict    = errors.New("vault item changed")
	ErrMissing     = errors.New("vault item is unavailable")
	ErrUnavailable = errors.New("vault key is unavailable")
	ErrInvalid     = errors.New("invalid vault entry")
)

// FileName is the embedded vault inside a store's folder, and the default name in a sync folder.
const FileName = "vault.kdbx"

// Store keeps the vault in a KeePass file in its own folder (the embedded copy). With a sync
// location set, that copy is merged both ways with the file there, and keeps working offline.
// Callers authorize metadata and replacement operations.
type Store struct {
	dir      string
	keys     Keys
	mu       sync.Mutex
	db       *gokeepasslib.Database
	password string
	local    stamp
	config   config
	sync     syncState
	now      func() time.Time
}

type config struct {
	Browser Browser `json:"browser"`
	Sync    string  `json:"sync,omitempty"`
}

// Open uses dir for the embedded vault and its settings; keys holds the vault password.
func Open(ctx context.Context, dir string, keys Keys) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, keys: keys, now: time.Now}
	if data, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		if err := json.Unmarshal(data, &s.config); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *Store) path() string { return filepath.Join(s.dir, FileName) }

// Close forgets the decrypted vault and its password.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db, s.password = nil, ""
}

// Status checks initialization and attempts to open the vault if one exists.
func (s *Store) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, created := stampOf(s.path())
	status := Status{Created: created, Protection: s.keys.Protection()}
	if created {
		status.Available = s.load(ctx) == nil
	}
	return status, nil
}

// Initialize creates a host-protected vault or verifies access to an existing one.
func (s *Store) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(ctx)
}

// load opens the embedded vault, creating it on first use, and brings in outside changes.
func (s *Store) load(ctx context.Context) error {
	if err := s.open(); err != nil {
		return err
	}
	s.pull()
	if s.db == nil {
		// Merging in the sync location's changes failed to save locally.
		return ErrUnavailable
	}
	return nil
}

func (s *Store) open() error {
	current, exists := stampOf(s.path())
	if s.db != nil && exists && current == s.local {
		return nil
	}
	if !exists {
		return s.create()
	}
	password, err := s.keys.Load()
	if err != nil || len(password) == 0 {
		return ErrUnavailable
	}
	db, err := readDatabase(s.path(), string(password))
	if err != nil {
		return ErrUnavailable
	}
	s.db, s.password, s.local = db, string(password), current
	return nil
}

// create starts an embedded vault with a random password. With a sync location set and its
// password still held, it starts from the synced file instead.
func (s *Store) create() error {
	if s.config.Sync != "" {
		if password, err := s.keys.Load(); err == nil && len(password) > 0 {
			if db, err := readDatabase(s.config.Sync, string(password)); err == nil {
				s.db, s.password = db, string(password)
				return s.writeLocal()
			}
		}
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	password := hex.EncodeToString(key)
	if err := s.keys.Save([]byte(password)); err != nil {
		return ErrUnavailable
	}
	s.db, s.password = newDatabase(password), password
	return s.writeLocal()
}

func (s *Store) writeLocal() error {
	data, err := encodeDatabase(s.db)
	if err == nil {
		err = writeAtomic(s.path(), data)
	}
	if err != nil {
		s.db = nil
		return err
	}
	s.local, _ = stampOf(s.path())
	return nil
}

// mutate applies change to the vault and saves it, then runs complete (a caller's
// acknowledgement), then syncs. A failed change or acknowledgement restores the previous
// file. Only a crash between the two can leave a save without its acknowledgement, so
// acknowledgements must tolerate repeats.
func (s *Store) mutate(ctx context.Context, change func() error, complete func() error) error {
	if err := s.load(ctx); err != nil {
		return err
	}
	if err := change(); err != nil {
		s.db = nil
		return err
	}
	previous, err := os.ReadFile(s.path())
	if err != nil {
		s.db = nil
		return err
	}
	if err := s.writeLocal(); err != nil {
		return err
	}
	if complete != nil {
		if err := complete(); err != nil {
			s.db = nil
			return errors.Join(err, writeAtomic(s.path(), previous))
		}
	}
	s.push()
	return nil
}

func (s *Store) root() *gokeepasslib.Group { return &s.db.Content.Root.Groups[0] }

// entries visits live entries, leaving out the recycle bin.
func (s *Store) entries(visit func(*gokeepasslib.Entry)) {
	recycle := s.db.Content.Meta.RecycleBinUUID
	var walk func(*gokeepasslib.Group)
	walk = func(group *gokeepasslib.Group) {
		if !recycle.IsZero() && group.UUID == recycle {
			return
		}
		for i := range group.Entries {
			visit(&group.Entries[i])
		}
		for i := range group.Groups {
			walk(&group.Groups[i])
		}
	}
	walk(s.root())
}

func (s *Store) find(id string) *gokeepasslib.Entry {
	var found *gokeepasslib.Entry
	s.entries(func(e *gokeepasslib.Entry) {
		if found == nil && entryID(e) == id {
			found = e
		}
	})
	return found
}

// List returns discoverable metadata without field values.
func (s *Store) List(ctx context.Context) ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return nil, err
	}
	items := []Summary{}
	s.entries(func(e *gokeepasslib.Entry) { items = append(items, Summarize(entryItem(e))) })
	slices.SortFunc(items, func(a, b Summary) int { return strings.Compare(a.Title, b.Title) })
	return items, nil
}

// Get returns plaintext only to trusted server-side adapters.
func (s *Store) Get(ctx context.Context, id string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(ctx, id)
}

func (s *Store) get(ctx context.Context, id string) (Item, error) {
	if err := s.load(ctx); err != nil {
		return Item{}, err
	}
	e := s.find(id)
	if e == nil {
		return Item{}, ErrMissing
	}
	return entryItem(e), nil
}

// Save validates an entry and requires its current revision, or revision zero for a new ID.
func (s *Store) Save(ctx context.Context, item Item) (Item, error) {
	return s.SaveWith(ctx, item, nil)
}

// SaveWith saves an entry, then runs complete.
func (s *Store) SaveWith(ctx context.Context, item Item, complete func() error) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := Validate(item); err != nil {
		return Item{}, err
	}
	var saved Item
	err := s.mutate(ctx, func() (err error) { saved, err = s.saveItem(item); return }, complete)
	return saved, err
}

// EditWith preserves hidden values unless replaced or explicitly removed.
func (s *Store) EditWith(ctx context.Context, item Item, removed []string, complete func() error) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.Revision > 0 {
		previous, err := s.get(ctx, item.ID)
		if err != nil {
			return Item{}, err
		}
		if previous.Revision != item.Revision {
			return Item{}, ErrConflict
		}
		for key, value := range item.Fields {
			if value != "" {
				previous.Fields[key] = value
			}
		}
		for _, key := range removed {
			delete(previous.Fields, key)
		}
		item.Fields = previous.Fields
		if item.Codes == nil {
			item.Codes = previous.Codes
		}
		for i := range item.Codes {
			if item.Codes[i].Value != "" {
				continue
			}
			for _, code := range previous.Codes {
				if code.ID == item.Codes[i].ID {
					item.Codes[i].Value = code.Value
					break
				}
			}
		}
	}
	item.Codes = slices.DeleteFunc(item.Codes, func(code Code) bool { return code.Value == "" })
	if err := Validate(item); err != nil {
		return Item{}, err
	}
	var saved Item
	err := s.mutate(ctx, func() (err error) { saved, err = s.saveItem(item); return }, complete)
	return saved, err
}

// save stores one item for an operation already holding the lock.
func (s *Store) save(ctx context.Context, item Item) (Item, error) {
	var saved Item
	err := s.mutate(ctx, func() (err error) { saved, err = s.saveItem(item); return }, nil)
	return saved, err
}

func (s *Store) saveItem(item Item) (Item, error) {
	now := w.TimeWrapper{Time: s.now().UTC()}
	e := s.find(item.ID)
	if item.Revision == 0 {
		if e != nil {
			return Item{}, ErrConflict
		}
		entry := gokeepasslib.NewEntry()
		if raw, err := hex.DecodeString(item.ID); err == nil && len(raw) == 16 {
			copy(entry.UUID[:], raw)
		}
		item.Revision = 1
		applyItem(&entry, Item{Fields: map[string]string{}}, item)
		entry.Times.LastModificationTime = &now
		s.root().Entries = append(s.root().Entries, entry)
		return item, nil
	}
	if e == nil {
		return Item{}, ErrConflict
	}
	before := entryItem(e)
	if before.Revision != item.Revision {
		return Item{}, ErrConflict
	}
	snapshot := copyEntry(e, s.db, s.db)
	snapshot.Histories = nil
	versions := []gokeepasslib.Entry{}
	for _, h := range e.Histories {
		versions = append(versions, h.Entries...)
	}
	e.Histories = []gokeepasslib.History{{Entries: trimHistory(s.db, append(versions, snapshot))}}
	item.Revision++
	applyItem(e, before, item)
	e.Times.LastModificationTime = &now
	return item, nil
}

// Delete removes only the specified revision; the host must authorize the mutation.
func (s *Store) Delete(ctx context.Context, id string, revision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(ctx, func() error {
		e := s.find(id)
		if e == nil || entryItem(e).Revision != revision {
			return ErrConflict
		}
		uuid := e.UUID
		var remove func(*gokeepasslib.Group) bool
		remove = func(group *gokeepasslib.Group) bool {
			for i := range group.Entries {
				if group.Entries[i].UUID == uuid {
					group.Entries = slices.Delete(group.Entries, i, i+1)
					return true
				}
			}
			for i := range group.Groups {
				if remove(&group.Groups[i]) {
					return true
				}
			}
			return false
		}
		remove(s.root())
		at := w.TimeWrapper{Time: s.now().UTC()}
		s.db.Content.Root.DeletedObjects = append(s.db.Content.Root.DeletedObjects, gokeepasslib.DeletedObjectData{UUID: uuid, DeletionTime: &at})
		return nil
	}, nil)
}

// Values resolves fields only inside a trusted adapter; a recovery code is reserved before dispatch.
func (s *Store) Values(ctx context.Context, id, origin string, fields []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	bound, err := Origin(item.URL)
	if err != nil || bound != origin {
		return nil, errors.New("vault item is bound to another origin")
	}
	values := map[string]string{}
	recovery := -1
	for _, field := range fields {
		switch field {
		case "privateKey", "totpSeed", "credentialID", "userHandle", "rpID", "signCount":
			return nil, errors.New("this field requires a dedicated vault operation")
		case "totp":
			otp, err := OTP(item.Fields["totp"], s.now())
			if err != nil {
				return nil, err
			}
			values[field] = otp.Code
		case "recovery":
			for i, code := range item.Codes {
				if !code.Used {
					values[field], recovery = code.Value, i
					break
				}
			}
		default:
			values[field] = item.Fields[field]
		}
		if values[field] == "" {
			return nil, errors.New("the requested vault field is unavailable")
		}
	}
	if recovery >= 0 {
		item.Codes[recovery].Used = true
		if _, err := s.save(ctx, item); err != nil {
			return nil, err
		}
	}
	return values, nil
}

// File resolves one field for a local hand-off to a tool that needs the value itself, so
// private key material is allowed. A recovery code is reserved like any other use.
func (s *Store) File(ctx context.Context, id, field string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.get(ctx, id)
	if err != nil {
		return "", err
	}
	switch field {
	case "":
		return "", errors.New("choose a vault field")
	case "totp":
		otp, err := OTP(item.Fields["totp"], s.now())
		if err != nil {
			return "", err
		}
		return otp.Code, nil
	case "recovery":
		for i, code := range item.Codes {
			if code.Used {
				continue
			}
			item.Codes[i].Used = true
			if _, err := s.save(ctx, item); err != nil {
				return "", err
			}
			return code.Value, nil
		}
		return "", errors.New("no unused recovery code")
	}
	value := item.Fields[field]
	if value == "" {
		return "", errors.New("the requested vault field is unavailable")
	}
	return value, nil
}

// Browser reads this host's Chromium endpoint configuration for a trusted adapter.
func (s *Store) Browser(ctx context.Context) (Browser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return Browser{}, err
	}
	return s.config.Browser, nil
}

// SetBrowser accepts only an explicit loopback Chromium debugging endpoint, or an empty endpoint.
func (s *Store) SetBrowser(ctx context.Context, browser Browser) error {
	if browser.Endpoint != "" {
		if _, err := endpoint(browser.Endpoint); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return err
	}
	next := s.config
	next.Browser = browser
	return s.saveConfig(next)
}

// saveConfig keeps per-host settings beside the vault; they are not synced.
func (s *Store) saveConfig(next config) error {
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(s.dir, "config.json"), data); err != nil {
		return err
	}
	s.config = next
	return nil
}

// MergeFile merges another vault file into this one the way a sync does, newer edits winning,
// without syncing with that file afterwards.
func (s *Store) MergeFile(ctx context.Context, path, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	other, err := readDatabase(path, password)
	if err != nil {
		return err
	}
	return s.mutate(ctx, func() error { mergeInto(s.db, other); return nil }, nil)
}

// importKey keys import fingerprints; it lives in the vault so every device dedupes alike.
func (s *Store) importKey() []byte {
	meta := s.db.Content.Meta
	for _, item := range meta.CustomData {
		if item.Key == "polymux.import" {
			if key, err := hex.DecodeString(item.Value); err == nil && len(key) == 32 {
				return key
			}
		}
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	meta.CustomData = append(meta.CustomData, gokeepasslib.CustomData{Key: "polymux.import", Value: hex.EncodeToString(key)})
	return key
}

func (s *Store) fingerprint(item Item) string {
	item.ID, item.Revision = "import", 0
	plain, _ := json.Marshal(item)
	digest := hmac.New(sha256.New, s.importKey())
	digest.Write([]byte("polymux-vault-import-v1"))
	digest.Write(plain)
	clear(plain)
	return "import_" + hex.EncodeToString(digest.Sum(nil))
}
