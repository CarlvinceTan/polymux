package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

func init() { kdfMemory, kdfIterations = 1<<20, 1 }

type testKeys struct{ key []byte }

func (k *testKeys) Load() ([]byte, error) { return slices.Clone(k.key), nil }
func (k *testKeys) Save(key []byte) error { k.key = slices.Clone(key); return nil }
func (*testKeys) Protection() string      { return "test" }

func testStore(t *testing.T) (*Store, string, *testKeys) {
	t.Helper()
	dir := t.TempDir()
	keys := &testKeys{}
	s, err := Open(t.Context(), dir, keys)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, dir, keys
}

func testItem() Item {
	return Item{ID: "test-entry", Kind: "login", Title: "Private account title", URL: "https://example.com/login",
		Fields: map[string]string{"username": "hidden-user-name", "password": "hidden-value-7ce2", "notes": "hidden-note-1299"}, Codes: []Code{}}
}

func TestEncryptedLifecycle(t *testing.T) {
	s, path, keys := testStore(t)
	ctx := t.Context()
	item, err := s.Save(ctx, testItem())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(metadata)
	for _, value := range item.Fields {
		if bytes.Contains(encoded, []byte(value)) {
			t.Fatal("metadata exposed a field value")
		}
	}
	data, err := os.ReadFile(filepath.Join(path, FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{item.Title, item.Fields["username"], item.Fields["password"], item.Fields["notes"]} {
		if bytes.Contains(data, []byte(value)) {
			t.Fatal("vault file contains plaintext vault content")
		}
	}
	s.Close()
	reopened, err := Open(ctx, path, keys)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Get(ctx, item.ID)
	if err != nil || got.Fields["password"] != item.Fields["password"] {
		t.Fatal("automatic use did not survive restart", err)
	}
	keys.key = nil
	reopened.Close()
	status, err := reopened.Status(ctx)
	if err != nil || status.Available || !status.Created {
		t.Fatal("missing machine key was hidden", err)
	}
	if _, err := reopened.Get(ctx, item.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing key did not fail closed", err)
	}
}

func TestHiddenEdits(t *testing.T) {
	s, _, _ := testStore(t)
	item := testItem()
	item.Codes = []Code{{ID: "recovery", Value: "synthetic-recovery"}}
	saved, err := s.Save(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	patch := Item{ID: saved.ID, Kind: saved.Kind, Title: "Renamed", URL: saved.URL, Revision: saved.Revision,
		Fields: map[string]string{"password": ""}, Codes: []Code{{ID: "recovery", Used: true}}}
	saved, err = s.EditWith(t.Context(), patch, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title != "Renamed" || saved.Fields["password"] != item.Fields["password"] || saved.Fields["username"] != item.Fields["username"] || saved.Codes[0].Value != "synthetic-recovery" || !saved.Codes[0].Used {
		t.Fatal("metadata edit lost a hidden value")
	}
	if _, err := s.EditWith(t.Context(), patch, nil, nil); !errors.Is(err, ErrConflict) {
		t.Fatal("stale edit accepted")
	}
	patch.Revision = saved.Revision
	patch.Fields["password"] = "replacement-password"
	saved, err = s.EditWith(t.Context(), patch, []string{"notes"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Fields["password"] != "replacement-password" || saved.Fields["notes"] != "" {
		t.Fatal("replacement or explicit clear ignored")
	}
}

func TestItemIntegrityAndConflicts(t *testing.T) {
	s, _, _ := testStore(t)
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	item, err := s.Save(t.Context(), testItem())
	if err != nil {
		t.Fatal(err)
	}
	stale := item
	item.Title = "Edited"
	if _, err := s.Save(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(t.Context(), stale); !errors.Is(err, ErrConflict) {
		t.Fatal("stale edit won")
	}
	if err := s.Delete(t.Context(), stale.ID, stale.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("stale delete won")
	}
	path := filepath.Join(s.dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-40] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(t.Context(), item.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("tampered vault was not refused", err)
	}
}

func TestRecoveryReservation(t *testing.T) {
	s, _, _ := testStore(t)
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	item := testItem()
	item.Codes = []Code{{ID: "first", Value: "first-code"}, {ID: "second", Value: "second-code"}}
	if _, err := s.Save(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	for _, fields := range [][]string{{"recovery", "unknown"}, {"privateKey"}, {"totpSeed"}} {
		if _, err := s.Values(t.Context(), item.ID, "https://example.com", fields); err == nil {
			t.Fatal("invalid references accepted")
		}
	}
	if _, err := s.Values(t.Context(), item.ID, "https://other.example", []string{"recovery"}); err == nil {
		t.Fatal("wrong origin accepted")
	}
	var group sync.WaitGroup
	values := make(chan string, 12)
	for range 12 {
		group.Go(func() {
			result, err := s.Values(t.Context(), item.ID, "https://example.com", []string{"recovery"})
			if err == nil {
				values <- result["recovery"]
			}
		})
	}
	group.Wait()
	close(values)
	seen := map[string]bool{}
	for value := range values {
		if seen[value] {
			t.Fatal("recovery code used twice")
		}
		seen[value] = true
	}
	if len(seen) != 2 {
		t.Fatal("recovery validation consumed a code")
	}
	stored, err := s.Get(t.Context(), item.ID)
	if err != nil || !stored.Codes[0].Used || !stored.Codes[1].Used {
		t.Fatal("reservation was not persisted", err)
	}
}
