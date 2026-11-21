package server

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/archive"
	"github.com/CarlvinceTan/polymux/apps/server/internal/network"
	"github.com/CarlvinceTan/polymux/apps/server/internal/secrets"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	vaultstore "github.com/CarlvinceTan/polymux/packages/vault"
)

func TestEveryServerTableHasAMergeRule(t *testing.T) {
	api := settingsAPI(t)
	rows, err := api.Store.DB.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'messages_fts_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rules := store.MergeRules()
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(rules, table) {
			t.Errorf("%s has no merge rule", table)
		}
	}
}

func TestEveryDurableTableHasAMergeRule(t *testing.T) {
	dir := t.TempDir()
	_, closeStorage, err := agentruntime.NewDurableStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStorage()
	db, err := openRead(filepath.Join(dir, "durable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		seen++
		if !slices.Contains(durableSkipped, table) {
			t.Errorf("durable table %s has no merge rule: a merge drops it silently", table)
		}
	}
	if seen == 0 {
		t.Fatal("durable.db has no tables, so this test proves nothing")
	}
}

func TestEveryDataFolderHasAMergeRule(t *testing.T) {
	api := settingsAPI(t)
	entries, err := os.ReadDir(api.config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	known := slices.Concat(mergeFolders, mergeLeftFolders)
	for _, entry := range entries {
		if entry.IsDir() && !slices.Contains(known, entry.Name()) {
			t.Errorf("data folder %s has no merge rule", entry.Name())
		}
	}
}

// mergeInto sends b's data to a as the worker does.
func mergeInto(t *testing.T, a, b *node) error {
	t.Helper()
	return mergeSealed(t, a, b, nil)
}

func mergeSealed(t *testing.T, a, b *node, keys []network.MovedKey) error {
	t.Helper()
	dir := b.api.config.DataDir
	note, err := json.Marshal(network.MoveNote{From: dir, Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, network.NoteFile), note, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.api.Store.DB.ExecContext(t.Context(), `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	peer, err := b.api.config.Devices.Store().Peer(t.Context(), a.api.config.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	check, err := b.api.peers().Check(t.Context(), peer)
	if err != nil {
		t.Fatal(err)
	}
	err = b.api.peers().Send(t.Context(), peer, check.Key, true, func(out io.Writer) error {
		// The real worker copies a stopped server; this one is live, so its logs are left out.
		return archive.Write(out, dir, func(name string) bool {
			return strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm")
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.api.peers().Commit(t.Context(), peer, true)
}

func TestMergeBringsAServerIn(t *testing.T) {
	a, b := startNode(t, "a"), startNode(t, "b")
	a.link(b)
	exited := make(chan struct{})
	a.api.config.Exit = func() { close(exited) }
	var created struct{ ID string }
	if status := b.call(b.owner, "POST", "/agents", `{"name":"Scout","role":"","prompt":""}`, &created); status >= 300 {
		t.Fatalf("create: %d", status)
	}
	if err := os.WriteFile(filepath.Join(b.api.config.DataDir, "attachments", "file1"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeInto(t, a, b); err != nil {
		t.Fatal(err)
	}
	<-exited
	var name string
	if err := a.api.Store.DB.QueryRowContext(t.Context(), `SELECT json_extract(data,'$.name') FROM agents WHERE id=?`, created.ID).Scan(&name); err != nil || name != "Scout" {
		t.Fatalf("bot %q %v", name, err)
	}
	if data, err := os.ReadFile(filepath.Join(a.api.config.DataDir, "attachments", "file1")); err != nil || string(data) != "kept" {
		t.Fatalf("attachment %q %v", data, err)
	}
	var info struct {
		Merged []string
		Peers  []any
	}
	a.call(a.owner, "GET", "/network", "", &info)
	if !slices.Contains(info.Merged, "b") || len(info.Peers) != 0 {
		t.Fatalf("after merge: %+v", info)
	}
	if _, err := os.Stat(a.api.incoming()); !os.IsNotExist(err) {
		t.Fatal("the copy stayed after the merge")
	}
	if _, err := os.Stat(filepath.Join(a.api.config.DataDir, "maintenance")); !os.IsNotExist(err) {
		t.Fatal("the server would restart sealed, refusing every write")
	}
}

func TestFailedMergeChangesNothing(t *testing.T) {
	a, b := startNode(t, "a"), startNode(t, "b")
	a.link(b)
	a.api.config.Exit = func() { t.Error("a failed merge stopped the server") }
	// The same ID on both is impossible by chance, so it stands in for anything that stops an import.
	for _, n := range []*node{a, b} {
		if _, err := n.api.Store.DB.ExecContext(t.Context(), `INSERT INTO agents(id,data) VALUES('same','{"name":"Twin"}')`); err != nil {
			t.Fatal(err)
		}
	}
	if err := mergeInto(t, a, b); err == nil {
		t.Fatal("a clash merged")
	}
	if state := a.api.admission.snapshot().State; state != "running" {
		t.Fatalf("server left %s", state)
	}
	if status := a.call(a.owner, "POST", "/agents", `{"name":"After","role":"","prompt":""}`, nil); status >= 300 {
		t.Fatalf("server didn't reopen: %d", status)
	}
	if !slices.Equal(a.peers(), []string{"b"}) {
		t.Fatalf("peers %v", a.peers())
	}
}

func TestMergePreviewNamesWhoseAccessChanges(t *testing.T) {
	a, b := startNode(t, "a"), startNode(t, "b")
	a.link(b)
	a.phone("only-on-a")
	token := b.phone("only-on-b")
	if status := b.call(token, "PATCH", "/network/devices/self", `{"name":"Tablet","client":"only-on-b"}`, nil); status != 204 {
		t.Fatalf("rename: %d", status)
	}
	var preview struct {
		Gains, Loses, Stays, Notes, Blockers []string
	}
	if status := b.call(b.owner, "GET", "/network/move?target=a&merge=1", "", &preview); status != 200 {
		t.Fatalf("preview: %d", status)
	}
	if len(preview.Blockers) != 0 {
		t.Fatalf("a target with data blocked a merge: %v", preview.Blockers)
	}
	if !slices.Contains(preview.Gains, "Phone") {
		t.Errorf("gains %v", preview.Gains)
	}
	if !slices.Contains(preview.Loses, "Tablet") {
		t.Errorf("loses %v", preview.Loses)
	}
	if len(preview.Stays) == 0 || len(preview.Notes) == 0 {
		t.Errorf("stays %v, notes %v", preview.Stays, preview.Notes)
	}
}

func TestMergeKeepsSecretValues(t *testing.T) {
	a, b := startNode(t, "a"), startNode(t, "b")
	a.link(b)
	exited := make(chan struct{})
	a.api.config.Exit = func() { close(exited) }
	keys := map[*node]*vaultKeys{a: {}, b: {}}
	for n, key := range keys {
		n.api.vault.Close()
		store, err := vaultstore.Open(t.Context(), filepath.Join(n.api.config.DataDir, "vault"), key)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Initialize(t.Context()); err != nil {
			t.Fatal(err)
		}
		n.api.vault = store
	}
	item := vaultstore.Item{ID: "c5e5a9f4-5e0e-4d7e-9a33-3f0d0f0d0f0d", Kind: "login", Title: "Kept", URL: "https://example.com",
		Fields: map[string]string{"username": "me", "password": "hunter2"}, Codes: []vaultstore.Code{}}
	if _, err := b.api.vault.Save(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	for _, credential := range []secrets.Credential{
		{Name: "plain", Origin: "https://example.com", Value: "no-kind"},
		{Name: "api", Kind: secrets.KindAPI, Origin: "https://example.com", Value: "api-value"},
		{Name: "ssh", Kind: secrets.KindSSH, Value: "ssh-value"},
	} {
		if err := b.api.secrets.Save("owner", credential); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.api.secrets.Save("owner", secrets.Credential{Name: "api", Origin: "https://example.com", Value: "mine"}); err != nil {
		t.Fatal(err)
	}
	if err := mergeSealed(t, a, b, []network.MovedKey{{Service: "com.polymux.vault", Suffix: "/password", Value: hex.EncodeToString(keys[b].value)}}); err != nil {
		t.Fatal(err)
	}
	<-exited
	got, err := a.api.vault.Get(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fields["password"] != "hunter2" || got.Fields["username"] != "me" {
		t.Fatalf("vault fields %v", got.Fields)
	}
	for name, want := range map[string]string{"plain": "no-kind", "ssh": "ssh-value", "api": "mine"} {
		credential, err := a.api.secrets.Get("owner", name)
		if err != nil || credential.Value != want {
			t.Errorf("%s = %q, %v; want %q", name, credential.Value, err, want)
		}
	}
}
