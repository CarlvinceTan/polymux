package secrets

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "credentials.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestVaultPersistence(t *testing.T) {
	dir, db := t.TempDir(), testDB(t)
	s, err := Open(dir, db)
	if err != nil {
		t.Fatal(err)
	}
	value := Credential{Name: "service", Origin: "https://api.example.com", Header: "Authorization", Scheme: "Bearer", Value: "vault-test-private-value"}
	if err := s.Save("a", value); err != nil {
		t.Fatal(err)
	}
	var data []byte
	if err := db.QueryRow(`SELECT data FROM credentials WHERE owner='a' AND name='service'`).Scan(&data); err != nil || bytes.Contains(data, []byte(value.Value)) {
		t.Fatal("credential row contains plaintext", err)
	}
	if info, err := os.Stat(filepath.Join(dir, "key")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential key permissions", err)
	}
	if values := s.List("a"); len(values) != 1 || values[0].Value != "" {
		t.Fatal("credential listing exposes values", values)
	}
	s, err = Open(dir, db)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("a", "service")
	if err != nil || got.Value != value.Value || got.Origin != value.Origin {
		t.Fatal("credential did not survive restart", err)
	}
	if _, err := s.Get("b", "service"); !errors.Is(err, ErrMissing) {
		t.Fatal("credential crossed owners", err)
	}
	if _, err := db.Exec(`INSERT INTO credentials(owner,name,data) SELECT 'b',name,data FROM credentials WHERE owner='a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("b", "service"); err == nil {
		t.Fatal("encrypted row was rebound to another owner")
	}
	if err := s.Delete("a", ""); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("a", "service"); !errors.Is(err, ErrMissing) {
		t.Fatal("deleted credential reappeared", err)
	}
}

func TestVaultTampering(t *testing.T) {
	dir, db := t.TempDir(), testDB(t)
	s, err := Open(dir, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("a", Credential{Name: "key", Value: "private"}); err != nil {
		t.Fatal(err)
	}
	var data []byte
	if err := db.QueryRow(`SELECT data FROM credentials WHERE owner='a'`).Scan(&data); err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if _, err := db.Exec(`UPDATE credentials SET data=? WHERE owner='a'`, data); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("a", "key"); err == nil {
		t.Fatal("tampered credential accepted")
	}
	if err := os.Remove(filepath.Join(dir, "key")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, db); err == nil {
		t.Fatal("missing key silently replaced")
	}
}

func TestMultiOriginAndExpiry(t *testing.T) {
	db := testDB(t)
	s, err := Open(t.TempDir(), db)
	if err != nil {
		t.Fatal(err)
	}
	now := int64(1_000_000)
	s.now = func() int64 { return now }
	gh := Credential{Name: "gh", Origin: "https://api.github.com", Origins: []string{"https://github.com"}, Value: "tok"}
	if err := s.Save("a", gh); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://api.github.com", "https://github.com"} {
		if !gh.Allows(origin) {
			t.Fatalf("secret not bound to %s", origin)
		}
	}
	if gh.Allows("https://evil.example") {
		t.Fatal("secret bound to an unrelated origin")
	}
	// A short-lived secret disappears once its expiry passes.
	if err := s.Save("a", Credential{Name: "temp", Origin: "https://x.example", ExpiresAt: now + 60_000, Value: "v"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("a", "temp"); err != nil {
		t.Fatal("fresh secret unavailable", err)
	}
	now += 61_000
	if _, err := s.Get("a", "temp"); !errors.Is(err, ErrMissing) {
		t.Fatal("expired secret still returned")
	}
	if len(s.List("a")) != 1 {
		t.Fatal("expired secret still listed")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM credentials WHERE owner='a' AND name='temp'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired secret not swept from disk", err)
	}
}

func TestEphemeralStaysInMemory(t *testing.T) {
	db := testDB(t)
	dir := t.TempDir()
	s, err := Open(dir, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("a", Credential{Name: "session", Origin: "https://x.example", Ephemeral: true, Value: "mem-only"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("a", "session")
	if err != nil || got.Value != "mem-only" {
		t.Fatal("ephemeral secret unavailable", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM credentials WHERE name='session'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("ephemeral secret reached the database", err)
	}
	// A reopened store (restart) has lost the memory-only secret.
	reopened, err := Open(dir, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Get("a", "session"); !errors.Is(err, ErrMissing) {
		t.Fatal("ephemeral secret survived restart")
	}
}
