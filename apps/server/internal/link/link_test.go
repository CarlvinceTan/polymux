package link

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityCreation(t *testing.T) {
	dir := t.TempDir()
	if err := createIdentity(filepath.Join(dir, "devices", "identity.ed25519")); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{"devices": 0700, "devices/identity.ed25519": 0600} {
		info, err := os.Stat(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatal("wrong permissions", info.Mode())
		}
	}
}

func TestIdentity(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.PrivateKey, second.PrivateKey) || !bytes.Equal(first.PublicKey, second.PublicKey) {
		t.Fatal("identity changed")
	}
	path := filepath.Join(dir, "devices", "identity.ed25519")
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIdentity(dir); err == nil {
		t.Fatal("regenerated corrupt identity")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "corrupt" {
		t.Fatal("modified corrupt identity")
	}
}
