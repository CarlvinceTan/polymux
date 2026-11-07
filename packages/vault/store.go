package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	ErrConflict    = errors.New("vault item changed")
	ErrMissing     = errors.New("vault item is unavailable")
	ErrUnavailable = errors.New("vault key is unavailable")
	ErrInvalid     = errors.New("invalid vault entry")
)

// Store owns encrypted records; callers authorize metadata and replacement operations.
type Store struct {
	db   *sql.DB
	keys Keys
	mu   sync.Mutex
	key  []byte
	now  func() time.Time
}

// Open creates the vault tables in a caller-owned SQLite database; keys supplies secure unattended access.
func Open(ctx context.Context, db *sql.DB, keys Keys) (*Store, error) {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS vault_meta (
 id INTEGER PRIMARY KEY CHECK(id=1), proof BLOB NOT NULL, browser BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS vault_items (
 id TEXT PRIMARY KEY, revision INTEGER NOT NULL, data BLOB NOT NULL);`)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, keys: keys, now: time.Now}, nil
}

// Close forgets the cached key, but does not close the caller's database.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.key)
	s.key = nil
}

// Status checks initialization and attempts to load the unattended key if a vault exists.
func (s *Store) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM vault_meta`).Scan(&count)
	status := Status{Created: count != 0, Protection: s.keys.Protection()}
	if err == nil {
		status.Available = s.loadKey(ctx) == nil
		status.Created = status.Created || status.Available
	}
	return status, err
}

// Initialize creates a host-protected vault or verifies access to an existing one.
func (s *Store) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadKey(ctx)
}

func (s *Store) create(ctx context.Context) error {
	key := random(32)
	defer clear(key)
	proof, err := seal(key, "proof", []byte("polymux-vault-v1"))
	if err != nil {
		return err
	}
	browser, err := seal(key, "browser", []byte(`{"endpoint":""}`))
	if err != nil {
		return err
	}
	if err := s.keys.Save(key); err != nil {
		return ErrUnavailable
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO vault_meta(id,proof,browser) VALUES(1,?,?)`, proof, browser); err != nil {
		return err
	}
	s.key = slices.Clone(key)
	return nil
}

// migrate removes the retired password wrapper after verifying the host key.
func (s *Store) migrate(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(vault_meta)`)
	if err != nil {
		return err
	}
	var password bool
	for rows.Next() {
		var id, required, primary int
		var name, kind string
		var value any
		if err := rows.Scan(&id, &name, &kind, &required, &value, &primary); err != nil {
			rows.Close()
			return err
		}
		if name == "salt" {
			password = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || !password {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `ALTER TABLE vault_meta DROP COLUMN salt`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE vault_meta DROP COLUMN wrapped`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) loadKey(ctx context.Context) error {
	if len(s.key) == 32 {
		return nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM vault_meta`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if err := s.migrate(ctx); err != nil {
			return err
		}
		return s.create(ctx)
	}
	key, err := s.keys.Load()
	if err != nil || len(key) != 32 {
		clear(key)
		return ErrUnavailable
	}
	defer clear(key)
	var proof []byte
	if err := s.db.QueryRowContext(ctx, `SELECT proof FROM vault_meta WHERE id=1`).Scan(&proof); err != nil {
		return ErrUnavailable
	}
	plain, err := open(key, "proof", proof)
	defer clear(plain)
	if err != nil || string(plain) != "polymux-vault-v1" {
		return ErrUnavailable
	}
	if err := s.migrate(ctx); err != nil {
		return err
	}
	s.key = slices.Clone(key)
	return nil
}

// List returns discoverable metadata without field values.
func (s *Store) List(ctx context.Context) ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadKey(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,revision,data FROM vault_items ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Summary{}
	for rows.Next() {
		var id string
		var revision int64
		var data []byte
		if err := rows.Scan(&id, &revision, &data); err != nil {
			return nil, err
		}
		item, err := s.decodeItem(id, revision, data)
		if err != nil {
			return nil, err
		}
		items = append(items, Summarize(item))
	}
	slices.SortFunc(items, func(a, b Summary) int { return strings.Compare(a.Title, b.Title) })
	return items, rows.Err()
}

// Get returns plaintext only to trusted server-side adapters.
func (s *Store) Get(ctx context.Context, id string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(ctx, id)
}

func (s *Store) get(ctx context.Context, id string) (Item, error) {
	if err := s.loadKey(ctx); err != nil {
		return Item{}, err
	}
	var revision int64
	var data []byte
	if err := s.db.QueryRowContext(ctx, `SELECT revision,data FROM vault_items WHERE id=?`, id).Scan(&revision, &data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrMissing
		}
		return Item{}, err
	}
	return s.decodeItem(id, revision, data)
}

func (s *Store) decodeItem(id string, revision int64, data []byte) (Item, error) {
	plain, err := open(s.key, itemAAD(id, revision), data)
	if err != nil {
		return Item{}, ErrUnavailable
	}
	defer clear(plain)
	var item Item
	if json.Unmarshal(plain, &item) != nil || item.ID != id || item.Revision != revision {
		return Item{}, ErrUnavailable
	}
	return item, nil
}

// Save validates an entry and requires its current revision, or revision zero for a new ID.
func (s *Store) Save(ctx context.Context, item Item) (Item, error) {
	return s.SaveWith(ctx, item, nil)
}

// SaveWith commits an edit and the optional callback in the same transaction.
func (s *Store) SaveWith(ctx context.Context, item Item, complete func(*sql.Tx) error) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := Validate(item); err != nil {
		return Item{}, err
	}
	if err := s.loadKey(ctx); err != nil {
		return Item{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	saved, err := s.saveItem(ctx, tx, item)
	if err != nil {
		return Item{}, err
	}
	if complete != nil {
		if err := complete(tx); err != nil {
			return Item{}, err
		}
	}
	return saved, tx.Commit()
}

// EditWith preserves hidden values unless replaced or explicitly removed.
func (s *Store) EditWith(ctx context.Context, item Item, removed []string, complete func(*sql.Tx) error) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadKey(ctx); err != nil {
		return Item{}, err
	}
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	saved, err := s.saveItem(ctx, tx, item)
	if err != nil {
		return Item{}, err
	}
	if complete != nil {
		if err := complete(tx); err != nil {
			return Item{}, err
		}
	}
	return saved, tx.Commit()
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Store) save(ctx context.Context, item Item) (Item, error) {
	if err := s.loadKey(ctx); err != nil {
		return Item{}, err
	}
	return s.saveItem(ctx, s.db, item)
}

func (s *Store) saveItem(ctx context.Context, db executor, item Item) (Item, error) {
	previous := item.Revision
	item.Revision++
	plain, err := json.Marshal(item)
	if err != nil {
		return Item{}, err
	}
	defer clear(plain)
	data, err := seal(s.key, itemAAD(item.ID, item.Revision), plain)
	if err != nil {
		return Item{}, err
	}
	var result sql.Result
	if previous == 0 {
		result, err = db.ExecContext(ctx, `INSERT INTO vault_items(id,revision,data) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, item.ID, item.Revision, data)
	} else {
		result, err = db.ExecContext(ctx, `UPDATE vault_items SET revision=?,data=? WHERE id=? AND revision=?`, item.Revision, data, item.ID, previous)
	}
	if err != nil {
		return Item{}, err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return Item{}, ErrConflict
	}
	return item, nil
}

// Delete removes only the specified revision; the host must authorize the mutation.
func (s *Store) Delete(ctx context.Context, id string, revision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `DELETE FROM vault_items WHERE id=? AND revision=?`, id, revision)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return ErrConflict
	}
	return nil
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

// Browser reads the encrypted Chromium endpoint configuration for a trusted adapter.
func (s *Store) Browser(ctx context.Context) (Browser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadKey(ctx); err != nil {
		return Browser{}, err
	}
	var data []byte
	if err := s.db.QueryRowContext(ctx, `SELECT browser FROM vault_meta WHERE id=1`).Scan(&data); err != nil {
		return Browser{}, err
	}
	plain, err := open(s.key, "browser", data)
	if err != nil {
		return Browser{}, ErrUnavailable
	}
	var browser Browser
	err = json.Unmarshal(plain, &browser)
	return browser, err
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
	if err := s.loadKey(ctx); err != nil {
		return err
	}
	plain, _ := json.Marshal(browser)
	data, err := seal(s.key, "browser", plain)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE vault_meta SET browser=? WHERE id=1`, data)
	return err
}

func random(n int) []byte {
	data := make([]byte, n)
	_, _ = rand.Read(data)
	return data
}

func seal(key []byte, aad string, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nil, plain, []byte("polymux:vault:1:"+aad)), nil
}

func open(key []byte, aad string, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nil, data, []byte("polymux:vault:1:"+aad))
}

func itemAAD(id string, revision int64) string { return fmt.Sprintf("item:%s:%d", id, revision) }
