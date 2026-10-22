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
)

var ErrMissing = errors.New("credential is unavailable")
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type Credential struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Header string `json:"header"`
	Scheme string `json:"scheme"`
	Value  string `json:"value,omitempty"`
}

type Store struct {
	db   *sql.DB
	aead cipher.AEAD
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
	return &Store{db: db, aead: aead}, nil
}

func (s *Store) Get(owner, name string) (Credential, error) {
	var data []byte
	if err := s.db.QueryRow(`SELECT data FROM credentials WHERE owner=? AND name=?`, owner, name).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Credential{}, ErrMissing
		}
		return Credential{}, err
	}
	return s.open(owner, name, data)
}

func (s *Store) List(owner string) []Credential {
	items := []Credential{}
	rows, err := s.db.Query(`SELECT name,data FROM credentials WHERE owner=? ORDER BY name`, owner)
	if err != nil {
		return items
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var data []byte
		if rows.Scan(&name, &data) != nil {
			continue
		}
		credential, err := s.open(owner, name, data)
		if err != nil {
			continue
		}
		credential.Value = ""
		items = append(items, credential)
	}
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
	data, err := s.Seal(owner, credential)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO credentials(owner,name,data) VALUES(?,?,?) ON CONFLICT(owner,name) DO UPDATE SET data=excluded.data`, owner, credential.Name, data)
	return err
}

func (s *Store) Delete(owner, name string) error {
	var err error
	if name == "" {
		_, err = s.db.Exec(`DELETE FROM credentials WHERE owner=?`, owner)
	} else {
		_, err = s.db.Exec(`DELETE FROM credentials WHERE owner=? AND name=?`, owner, name)
	}
	return err
}
