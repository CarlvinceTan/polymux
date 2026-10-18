package link

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

type Identity struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

func LoadIdentity(dir string) (Identity, error) {
	path := filepath.Join(dir, "devices", "identity.ed25519")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := createIdentity(path); err != nil {
			return Identity{}, err
		}
		return LoadIdentity(dir)
	}
	if err != nil {
		return Identity{}, err
	}
	if len(data) != ed25519.PrivateKeySize || !bytes.Equal(data, ed25519.NewKeyFromSeed(data[:ed25519.SeedSize])) {
		return Identity{}, errors.New("invalid device identity; explicitly reset it")
	}
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		return Identity{}, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return Identity{}, err
	}
	key := ed25519.PrivateKey(data)
	return Identity{PublicKey: key.Public().(ed25519.PublicKey), PrivateKey: key}, nil
}

func createIdentity(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		return err
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	_, err = f.Write(key)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Publish only a complete key; concurrent creators must use the winner.
	if err := os.Link(f.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}
