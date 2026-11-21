package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Two devices share a folder that something else syncs; each keeps its embedded copy.
func TestSyncFolder(t *testing.T) {
	ctx := t.Context()
	shared := filepath.Join(t.TempDir(), "iCloud")
	if err := os.Mkdir(shared, 0700); err != nil {
		t.Fatal(err)
	}
	a, _, aKeys := testStore(t)
	b, _, bKeys := testStore(t)
	clock := time.Now()
	tick := func() time.Time { clock = clock.Add(2 * time.Second); return clock }
	a.now, b.now = tick, tick

	first, err := a.Save(ctx, testItem())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetSync(ctx, shared, "short"); err == nil {
		t.Fatal("a new synced vault accepted a weak password")
	}
	status, err := a.SetSync(ctx, shared, "shared-password")
	if err != nil || status.State != "synced" || string(aKeys.key) != "shared-password" {
		t.Fatal("sync was not set up", status, err)
	}
	if _, err := b.SetSync(ctx, shared, "wrong-password"); !errors.Is(err, ErrKDBX) {
		t.Fatal("a wrong password joined", err)
	}
	other := testItem()
	other.ID, other.Title = "b-only", "Only on B"
	if _, err := b.Save(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := b.SetSync(ctx, shared, "shared-password"); err != nil || string(bKeys.key) != "shared-password" {
		t.Fatal("joining failed", err)
	}
	if _, err := b.Get(ctx, first.ID); err != nil {
		t.Fatal("B did not get A's entry", err)
	}
	if _, err := a.Get(ctx, "b-only"); err != nil {
		t.Fatal("A did not get B's entry", err)
	}

	// An edit on B reaches A, and A's stale copy of it is refused.
	stale, _ := a.Get(ctx, first.ID)
	edit, _ := b.Get(ctx, first.ID)
	edit.Fields["password"] = "edited-on-b"
	if _, err := b.Save(ctx, edit); err != nil {
		t.Fatal(err)
	}
	got, err := a.Get(ctx, first.ID)
	if err != nil || got.Fields["password"] != "edited-on-b" {
		t.Fatal("edit did not sync", err)
	}
	stale.Title = "Stale"
	if _, err := a.Save(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatal("a stale edit overwrote a synced one", err)
	}

	// Offline: A keeps working on its embedded copy and catches up later.
	hidden := shared + ".away"
	if err := os.Rename(shared, hidden); err != nil {
		t.Fatal(err)
	}
	offline := testItem()
	offline.ID, offline.Title = "offline", "Made offline"
	if _, err := a.Save(ctx, offline); err != nil {
		t.Fatal("offline save failed", err)
	}
	if status, _ := a.Sync(ctx); status.State != "unavailable" {
		t.Fatal("offline state not reported", status)
	}
	if err := os.Rename(hidden, shared); err != nil {
		t.Fatal(err)
	}
	if status, _ := a.Sync(ctx); status.State != "synced" {
		t.Fatal("did not catch up", status)
	}
	if _, err := b.Get(ctx, "offline"); err != nil {
		t.Fatal("offline entry did not reach B", err)
	}

	// Deletes carry over instead of coming back.
	gone, _ := b.Get(ctx, "b-only")
	if err := b.Delete(ctx, gone.ID, gone.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Get(ctx, "b-only"); !errors.Is(err, ErrMissing) {
		t.Fatal("delete did not sync", err)
	}
	if _, err := b.Get(ctx, "b-only"); !errors.Is(err, ErrMissing) {
		t.Fatal("deleted entry came back", err)
	}

	// A conflict copy left by the sync service is merged and removed.
	conflict := filepath.Join(shared, "vault 2.kdbx")
	c, _, cKeys := testStore(t)
	c.now = tick
	cKeys.key = []byte("shared-password")
	if _, err := c.SetSync(ctx, conflict, ""); err == nil {
		t.Fatal("a missing file joined without a password")
	}
	if _, err := c.SetSync(ctx, conflict, "shared-password"); err != nil {
		t.Fatal(err)
	}
	extra := testItem()
	extra.ID, extra.Title = "from-conflict", "From a conflict copy"
	if _, err := c.Save(ctx, extra); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, err := a.Get(ctx, "from-conflict"); err != nil {
		t.Fatal("conflict copy not merged", err)
	}
	if _, err := os.Stat(conflict); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("conflict copy left behind")
	}

	// Turning sync off keeps the vault.
	if status, err := a.SetSync(ctx, "", ""); err != nil || status.State != "off" {
		t.Fatal(err)
	}
	if _, err := a.Get(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
}

// A sync folder that can't be written keeps changes pending without losing the vault.
func TestSyncUnwritable(t *testing.T) {
	ctx := t.Context()
	shared := filepath.Join(t.TempDir(), "iCloud")
	if err := os.Mkdir(shared, 0700); err != nil {
		t.Fatal(err)
	}
	s, _, _ := testStore(t)
	if _, err := s.SetSync(ctx, shared, "shared-password"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(shared, 0700) })
	item, err := s.Save(ctx, testItem())
	if err != nil {
		t.Fatal(err)
	}
	if items, err := s.List(ctx); err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if status, _ := s.Sync(ctx); status.State != "pending" {
		t.Fatal("expected pending, got", status.State)
	}

	if err := os.Chmod(shared, 0700); err != nil {
		t.Fatal(err)
	}
	if status, err := s.Sync(ctx); err != nil || status.State != "synced" {
		t.Fatal(status, err)
	}
	other, _, otherKeys := testStore(t)
	otherKeys.key = []byte("shared-password")
	if _, err := other.SetSync(ctx, shared, "shared-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Get(ctx, item.ID); err != nil {
		t.Fatal("pending change never reached the folder", err)
	}
}
