package hub

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const keyService = "com.polymux.hub"

var errLocked = publicError("Hub can't read its saved sign-ins.")

// keyStore holds the key that seals saved credentials; load reports os.ErrNotExist before one is made.
type keyStore interface {
	load() ([]byte, error)
	save(key []byte) error
}

// hostKey keeps the key out of Hub's files: in the Keychain on a Mac, scoped to this
// install, and in an owner-only file elsewhere, as the server's vault does.
type hostKey struct{ dir string }

func (k hostKey) account() string {
	dir, _ := filepath.Abs(k.dir)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(dir)))
}

func (k hostKey) path() string { return filepath.Join(k.dir, "credentials.key") }

func (k hostKey) load() ([]byte, error) {
	if runtime.GOOS != "darwin" {
		info, err := os.Lstat(k.path())
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, errLocked
		}
		return os.ReadFile(k.path())
	}
	output, err := keychain("", "find-generic-password", "-s", keyService, "-a", k.account(), "-w")
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 44 {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimSpace(string(output)))
}

func (k hostKey) save(key []byte) error {
	if runtime.GOOS != "darwin" {
		return writePrivate(k.path(), key)
	}
	// The interactive protocol keeps the key out of argv and process listings.
	input := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", keyService, k.account(), hex.EncodeToString(key))
	_, err := keychain(input, "-i")
	return err
}

func keychain(input string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	cmd.Stdin = strings.NewReader(input)
	return cmd.Output()
}

// writePrivate replaces a file in one step, so a crash can't leave half of it.
func writePrivate(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	if err := errors.Join(err, file.Chmod(0o600), file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// sealer encrypts credentials with AES-256-GCM, each bound to the record it belongs to.
type sealer struct {
	keys keyStore

	mu   sync.Mutex
	aead cipher.AEAD
}

// cipher only makes a new key when asked, so a Keychain that's locked now can't orphan what was sealed before.
func (s *sealer) cipher(create bool) (cipher.AEAD, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aead != nil {
		return s.aead, nil
	}
	key, err := s.keys.load()
	if errors.Is(err, os.ErrNotExist) && create {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err == nil {
			err = s.keys.save(key)
		}
	}
	if err != nil || len(key) != 32 {
		return nil, errors.Join(errLocked, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	s.aead, err = cipher.NewGCM(block)
	return s.aead, err
}

func (s *sealer) seal(id string, value any) ([]byte, error) {
	aead, err := s.cipher(true)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, []byte(id)), nil
}

func (s *sealer) open(id string, sealed []byte, value any) error {
	aead, err := s.cipher(false)
	if err != nil {
		return err
	}
	if len(sealed) < aead.NonceSize() {
		return errLocked
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(id))
	if err != nil {
		return errors.Join(errLocked, err)
	}
	return json.Unmarshal(plain, value)
}
