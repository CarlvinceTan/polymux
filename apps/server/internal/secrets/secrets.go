package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var ErrMissing = errors.New("credential is unavailable")
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

const (
	KindAPI = "api"
	KindSSH = "ssh"
)

type Credential struct {
	Name string `json:"name"`
	// Kind is KindAPI for an HTTP credential and KindSSH for a private key.
	Kind   string `json:"kind,omitempty"`
	Origin string `json:"origin"`
	// Origins are extra bound origins, so one secret can serve a tool that spans hosts, like gh.
	Origins []string `json:"origins,omitempty"`
	Header  string   `json:"header"`
	Scheme  string   `json:"scheme"`
	// ExpiresAt is a unix-milli expiry; 0 never expires.
	ExpiresAt int64 `json:"expiresAt,omitempty"`
	// Ephemeral secrets live only in memory and are never written to disk.
	Ephemeral bool   `json:"ephemeral,omitempty"`
	Value     string `json:"value,omitempty"`
}

// Allows reports whether this secret is bound to origin.
func (c Credential) Allows(origin string) bool {
	if origin == c.Origin {
		return true
	}
	for _, bound := range c.Origins {
		if bound == origin {
			return true
		}
	}
	return false
}

func (c Credential) expired(now int64) bool { return c.ExpiresAt > 0 && now >= c.ExpiresAt }

// IsSSH reports whether this credential is a private key rather than an HTTP credential.
func (c Credential) IsSSH() bool { return c.Kind == KindSSH }

type Store struct {
	db   *sql.DB
	aead cipher.AEAD
	now  func() int64
	mu   sync.Mutex
	// memory holds ephemeral secrets by owner then name; they never reach the database.
	memory map[string]map[string]Credential
}

func ValidName(name string) bool { return namePattern.MatchString(name) }

func Open(dir string, db *sql.DB) (*Store, error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS credentials (owner TEXT NOT NULL, name TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(owner,name))`); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, "key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM credentials`).Scan(&count); err != nil {
			return nil, err
		}
		if count != 0 {
			return nil, errors.New("credential encryption key is missing")
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		_, writeErr := file.Write(key)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if err := os.Chmod(keyPath, 0600); err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid credential encryption key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, aead: aead, now: func() int64 { return time.Now().UnixMilli() }, memory: map[string]map[string]Credential{}}, nil
}

func (s *Store) Get(owner, name string) (Credential, error) {
	if credential, ok := s.fromMemory(owner, name); ok {
		return credential, nil
	}
	var data []byte
	if err := s.db.QueryRow(`SELECT data FROM credentials WHERE owner=? AND name=?`, owner, name).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Credential{}, ErrMissing
		}
		return Credential{}, err
	}
	credential, err := s.open(owner, name, data)
	if err != nil {
		return Credential{}, err
	}
	if credential.expired(s.now()) {
		_ = s.Delete(owner, name)
		return Credential{}, ErrMissing
	}
	return credential, nil
}

// fromMemory returns a live ephemeral secret, sweeping it when expired.
func (s *Store) fromMemory(owner, name string) (Credential, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	credential, ok := s.memory[owner][name]
	if !ok {
		return Credential{}, false
	}
	if credential.expired(s.now()) {
		delete(s.memory[owner], name)
		return Credential{}, false
	}
	return credential, true
}

func (s *Store) List(owner string) []Credential {
	return s.list(owner, false)
}

// All returns every unexpired credential the owner has, with values, whatever its kind. Callers
// must never return them to the model.
func (s *Store) All(owner string) []Credential { return s.list(owner, true) }

// Values returns the owner's unexpired credentials of kind with their values, for the SSH
// agent to sign with; callers must never return them to the model.
func (s *Store) Values(owner, kind string) []Credential {
	items := []Credential{}
	for _, credential := range s.list(owner, true) {
		if credential.Kind == kind {
			items = append(items, credential)
		}
	}
	return items
}

func (s *Store) list(owner string, values bool) []Credential {
	now := s.now()
	items := []Credential{}
	rows, err := s.db.Query(`SELECT name,data FROM credentials WHERE owner=? ORDER BY name`, owner)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var data []byte
			if rows.Scan(&name, &data) != nil {
				continue
			}
			credential, err := s.open(owner, name, data)
			if err != nil || credential.expired(now) {
				continue
			}
			if !values {
				credential.Value = ""
			}
			items = append(items, credential)
		}
	}
	s.mu.Lock()
	for _, credential := range s.memory[owner] {
		if !credential.expired(now) {
			if !values {
				credential.Value = ""
			}
			items = append(items, credential)
		}
	}
	s.mu.Unlock()
	return items
}

func (s *Store) Seal(owner string, credential Credential) ([]byte, error) {
	if owner == "" || !ValidName(credential.Name) || credential.Value == "" || len(credential.Value) > 16384 {
		return nil, errors.New("invalid credential")
	}
	plain, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plain, []byte(owner+"\x00"+credential.Name)), nil
}

func (s *Store) open(owner, name string, data []byte) (Credential, error) {
	var credential Credential
	if len(data) < s.aead.NonceSize() {
		return credential, errors.New("invalid encrypted credential")
	}
	plain, err := s.aead.Open(nil, data[:s.aead.NonceSize()], data[s.aead.NonceSize():], []byte(owner+"\x00"+name))
	if err != nil {
		return credential, errors.New("cannot decrypt credential")
	}
	if err := json.Unmarshal(plain, &credential); err != nil {
		return Credential{}, errors.New("invalid encrypted credential")
	}
	if credential.Name != name {
		return Credential{}, errors.New("credential identity mismatch")
	}
	return credential, nil
}

func (s *Store) Save(owner string, credential Credential) error {
	if credential.Ephemeral {
		if owner == "" || !ValidName(credential.Name) || credential.Value == "" || len(credential.Value) > 16384 {
			return errors.New("invalid credential")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.memory[owner] == nil {
			s.memory[owner] = map[string]Credential{}
		}
		// A memory-only secret replaces any saved one of the same name, and vice versa.
		s.memory[owner][credential.Name] = credential
		_, _ = s.db.Exec(`DELETE FROM credentials WHERE owner=? AND name=?`, owner, credential.Name)
		return nil
	}
	data, err := s.Seal(owner, credential)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec(`INSERT INTO credentials(owner,name,data) VALUES(?,?,?) ON CONFLICT(owner,name) DO UPDATE SET data=excluded.data`, owner, credential.Name, data); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.memory[owner], credential.Name)
	s.mu.Unlock()
	return nil
}

func (s *Store) Delete(owner, name string) error {
	s.mu.Lock()
	if name == "" {
		delete(s.memory, owner)
	} else {
		delete(s.memory[owner], name)
	}
	s.mu.Unlock()
	var err error
	if name == "" {
		_, err = s.db.Exec(`DELETE FROM credentials WHERE owner=?`, owner)
	} else {
		_, err = s.db.Exec(`DELETE FROM credentials WHERE owner=? AND name=?`, owner, name)
	}
	return err
}
