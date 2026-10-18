package device

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalCredential(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	token, err := s.Local(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Local(t.Context(), dir)
	if err != nil || again != token {
		t.Fatal("local credential changed")
	}
	info, err := os.Stat(filepath.Join(dir, "devices", "local-token"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential must be owner-only")
	}
	pair, err := s.Authenticate(t.Context(), token)
	if err != nil || len(pair.Capabilities) != 0 {
		t.Fatal("local credential granted native capabilities")
	}
	if err := s.Revoke(t.Context(), pair.ID); err != nil {
		t.Fatal(err)
	}
	replacement, err := s.Local(t.Context(), dir)
	if err != nil || replacement == token {
		t.Fatal("restart must issue a fresh owner credential")
	}
	if _, err := s.Authenticate(t.Context(), token); err == nil {
		t.Fatal("revoked credential accepted")
	}
	owner, err := s.Authenticate(t.Context(), replacement)
	if err != nil || len(owner.Capabilities) != 0 {
		t.Fatal("replacement must authenticate without native capabilities")
	}
}

func TestLocalReplacesStaleToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "devices"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devices", "local-token"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	token, err := s.Local(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if token == "stale" {
		t.Fatal("stale token was reused")
	}
	if _, err := s.Authenticate(t.Context(), token); err != nil {
		t.Fatal("fresh token does not authenticate")
	}
}
