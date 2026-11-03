package vault_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/CarlvinceTan/polymux/packages/vault"
)

func TestStoredFormat(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "existing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	// Build the pre-extraction schema and ciphertext without using vault's writers.
	_, err = db.ExecContext(ctx, `
CREATE TABLE vault_meta (id INTEGER PRIMARY KEY CHECK(id=1), salt BLOB NOT NULL,
 wrapped BLOB NOT NULL, proof BLOB NOT NULL, browser BLOB NOT NULL);
CREATE TABLE vault_items (id TEXT PRIMARY KEY, revision INTEGER NOT NULL, data BLOB NOT NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{7}, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	seal := func(aad, plain string) []byte {
		nonce := make([]byte, gcm.NonceSize())
		_, _ = rand.Read(nonce)
		return gcm.Seal(nonce, nonce, []byte(plain), []byte("polymux:vault:1:"+aad))
	}
	_, err = db.ExecContext(ctx, `INSERT INTO vault_meta VALUES(1,?,?,?,?)`, []byte{}, []byte{},
		seal("proof", "polymux-vault-v1"), seal("browser", `{"endpoint":""}`))
	if err != nil {
		t.Fatal(err)
	}
	plain := `{"id":"existing","kind":"login","title":"Existing entry","url":"https://example.com","fields":{"password":"synthetic-existing-password"},"codes":[{"id":"r1","value":"synthetic-recovery","used":false}],"revision":9}`
	_, err = db.ExecContext(ctx, `INSERT INTO vault_items VALUES('existing',9,?)`, seal("item:existing:9", plain))
	if err != nil {
		t.Fatal(err)
	}
	keys := &memoryKeys{}
	store, err := vault.Open(ctx, db, keys)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Get(ctx, "existing"); !errors.Is(err, vault.ErrUnavailable) {
		t.Fatal("missing key did not fail closed", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('vault_meta') WHERE name='salt'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("unavailable vault was migrated", err)
	}
	keys.key = key
	item, err := store.Get(ctx, "existing")
	if err != nil || item.Revision != 9 || item.Fields["password"] != "synthetic-existing-password" || len(item.Codes) != 1 {
		t.Fatalf("existing record unreadable: revision=%d err=%v", item.Revision, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('vault_meta') WHERE name IN ('salt','wrapped')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("retired password wrapper remains", err)
	}
	item.Title = "Edited entry"
	saved, err := store.Save(ctx, item)
	if err != nil || saved.Revision != 10 {
		t.Fatalf("existing record cannot be updated: revision=%d err=%v", saved.Revision, err)
	}
	var data []byte
	if err := db.QueryRowContext(ctx, `SELECT data FROM vault_items WHERE id='existing'`).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("synthetic-existing-password")) {
		t.Fatal("updated record is not encrypted")
	}
	nonce := data[:gcm.NonceSize()]
	if _, err := gcm.Open(nil, nonce, data[gcm.NonceSize():], []byte("polymux:vault:1:item:existing:10")); err != nil {
		t.Fatal("updated record no longer uses the existing envelope", err)
	}
	store.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("closing the vault closed its caller-owned database", err)
	}
}
