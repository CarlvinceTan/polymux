package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/MichaelKinsy/PiG/durable"
	dsession "github.com/MichaelKinsy/PiG/durable/session"
	"github.com/MichaelKinsy/PiG/durable/storage/sqlite/node"
)

func TestDocuments(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	first, err := s.Save(t.Context(), "a", "a", Document{Name: "preference", Text: "Explicit preference: short replies", Source: "user-1"})
	if err != nil || first.Revision == "" {
		t.Fatal("create", first, err)
	}
	for _, scope := range []struct{ owner, scope string }{{"a", "group"}, {"b", "a"}} {
		items, err := s.List(t.Context(), scope.owner, scope.scope)
		if err != nil || len(items) != 0 {
			t.Fatal("private memory crossed scope", items, err)
		}
	}
	if _, err := s.Save(t.Context(), "a", "a", Document{Name: first.Name, Text: "stale create"}); !errors.Is(err, ErrConflict) {
		t.Fatal("unconditional overwrite", err)
	}
	updated := first
	updated.Text = "Explicit correction: detailed replies"
	updated, err = s.Save(t.Context(), "a", "a", updated)
	if err != nil || updated.Revision == first.Revision {
		t.Fatal("update", updated, err)
	}
	if err := s.Forget(t.Context(), "a", "a", first.Name, first.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("stale deletion", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = New(root)
	defer s.Close()
	items, err := s.List(t.Context(), "a", "a")
	if err != nil || len(items) != 1 || items[0] != updated {
		t.Fatal("reopen", items, err)
	}
	if err := s.Forget(t.Context(), "a", "a", updated.Name, updated.Revision); err != nil {
		t.Fatal("forget", err)
	}
	if _, err := s.Save(t.Context(), "a", "a", updated); !errors.Is(err, ErrConflict) {
		t.Fatal("stale writer resurrected forgotten memory", err)
	}
	new, err := s.Save(t.Context(), "a", "a", Document{Name: first.Name, Text: "New explicit request"})
	if err != nil || new.Revision == updated.Revision {
		t.Fatal("recreation", new, err)
	}
	if err := s.Delete(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted agent retained memory database", err)
	}
	if _, err := s.List(t.Context(), "a", "a"); err == nil {
		t.Fatal("deleted owner reopened")
	}
}

func TestConcurrentEdits(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	doc, err := s.Save(t.Context(), "a", "a", Document{Name: "fact", Text: "original"})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, text := range []string{"first edit", "second edit"} {
		wg.Go(func() {
			input := doc
			input.Text = text
			_, err := s.Save(t.Context(), "a", "a", input)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	conflicts := 0
	for err := range results {
		if errors.Is(err, ErrConflict) {
			conflicts++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if conflicts != 1 {
		t.Fatal("expected exactly one stale edit", conflicts)
	}
}

type failedStorage struct {
	durable.Storage
	err error
}

func (s *failedStorage) Close(ctx context.Context) error {
	return errors.Join(s.Storage.Close(ctx), s.err)
}

func TestDeleteCloseFailureCanRetry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "memory.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	storage, err := node.OpenNodeSqliteStorage(path, node.NodeSqliteStorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	failed := errors.New("storage close failed")
	s := New(root)
	s.sessions["a"] = dsession.CreateSession(&failedStorage{Storage: storage, err: failed})
	defer s.Close()
	if err := s.Delete(t.Context(), "a"); !errors.Is(err, failed) {
		t.Fatal("close error not reported", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("terminal close error skipped removal", err)
	}
	if err := s.Delete(t.Context(), "a"); err != nil {
		t.Fatal("cached close failure prevented retry", err)
	}
}

func TestDeleteCancelledCloseRetainsHandle(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "memory.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	storage, err := node.OpenNodeSqliteStorage(path, node.NodeSqliteStorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	closing := make(chan struct{})
	s := New(root)
	s.sessions["a"] = dsession.NewSessionImpl(storage, dsession.Hooks{BeforeClose: func() { <-closing }})
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = s.Delete(ctx, "a")
	close(closing)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled close", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("removed an open database", err)
	}
	if err := s.Delete(t.Context(), "a"); err != nil {
		t.Fatal("close could not settle on retry", err)
	}
}

func TestIdenticalWrites(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := New(root)
	t.Cleanup(func() { s.Close() })
	first, err := s.Save(t.Context(), "a", "chat", Document{Name: "preference", Text: "Short replies", Source: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.Save(t.Context(), "a", "chat", Document{Name: "style", Text: first.Text, Source: "user-2"})
	if err != nil || duplicate != first {
		t.Fatal("duplicate changed canonical record", duplicate, err)
	}
	for _, scope := range []struct{ owner, scope string }{{"a", "other"}, {"b", "chat"}} {
		if _, err := s.Save(t.Context(), scope.owner, scope.scope, Document{Name: "style", Text: first.Text}); err != nil {
			t.Fatal("duplicate crossed scope", err)
		}
		items, err := s.List(t.Context(), scope.owner, scope.scope)
		if err != nil || len(items) != 1 || items[0].Name != "style" {
			t.Fatal("scope missing its own record", items, err)
		}
	}
	other, err := s.Save(t.Context(), "a", "chat", Document{Name: "other", Text: "Detailed replies"})
	if err != nil {
		t.Fatal(err)
	}
	other.Text = first.Text
	stale := other
	stale.Revision = "stale"
	if _, err := s.Save(t.Context(), "a", "chat", stale); !errors.Is(err, ErrConflict) {
		t.Fatal("dedupe bypassed revision check", err)
	}
	merged, err := s.Save(t.Context(), "a", "chat", other)
	if err != nil || merged != first {
		t.Fatal("edit did not merge identical content", merged, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = New(root)
	items, err := s.List(t.Context(), "a", "chat")
	if err != nil || len(items) != 1 || items[0] != first {
		t.Fatal("reopened duplicates", items, err)
	}
	if err := s.Forget(t.Context(), "a", "chat", first.Name, first.Revision); err != nil {
		t.Fatal("canonical revision unusable", err)
	}
	items, err = s.List(t.Context(), "a", "chat")
	if err != nil || len(items) != 0 {
		t.Fatal("forgotten fact still recalled", items, err)
	}
}

func TestConcurrentDuplicates(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir())
	defer s.Close()
	results := make(chan Document, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"first", "second"} {
		wg.Go(func() {
			doc, err := s.Save(t.Context(), "a", "chat", Document{Name: name, Text: "Same fact"})
			results <- doc
			errs <- err
		})
	}
	wg.Wait()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	first, second := <-results, <-results
	items, err := s.List(t.Context(), "a", "chat")
	if err != nil || first != second || len(items) != 1 || items[0] != first {
		t.Fatal("concurrent writes duplicated fact", first, second, items, err)
	}
}
