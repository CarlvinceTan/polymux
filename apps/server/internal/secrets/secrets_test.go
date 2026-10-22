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
	if err != nil || got != value {
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
