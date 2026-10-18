package search

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type delayedEmbedding struct {
	started chan struct{}
	release chan struct{}
}

func (e delayedEmbedding) Embed(context.Context, Input) ([]float32, string, error) {
	close(e.started)
	<-e.release
	return []float32{1, 0, 0}, "test", nil
}

func TestDeletedAgentCannotBeReindexedAfterEmbedding(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	text := "remove all search entries"
	m := contract.Message{ID: "one", ThreadID: "a", Role: "user", Status: "done", Parts: []contract.Part{{Kind: "text", Text: &text}}}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	embedding := delayedEmbedding{started: make(chan struct{}), release: make(chan struct{})}
	index := &Index{Store: s, Embedder: embedding, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	finished := make(chan error, 1)
	go func() { finished <- index.Ingest(t.Context(), m) }()
	<-embedding.started
	if _, _, err := s.DeleteAgent(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	close(embedding.release)
	if err := <-finished; !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale embedding did not detect deleted message: %v", err)
	}
	for _, table := range []string{"messages", "messages_fts", "media_index", "media_vectors"} {
		var count int
		if err := s.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("recreated deleted data table=%s count=%d err=%v", table, count, err)
		}
	}
}

func TestDeletedAttachmentCannotReturnAfterExtraction(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	a := contract.Attachment{ID: "image", Kind: "image", MimeType: "image/png", IndexingState: "pending"}
	if err := s.SaveAttachment(t.Context(), a, "/tmp/deleted-test-image"); err != nil {
		t.Fatal(err)
	}
	m := contract.Message{ID: "one", ThreadID: "a", Role: "user", Status: "done", Parts: []contract.Part{{Kind: "attachment", Attachment: &a}}}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	index := &Index{
		Store: s,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Extractor: ExtractorFunc(func(context.Context, Asset) (string, error) {
			close(started)
			<-release
			return "late extraction", nil
		}),
	}
	finished := make(chan error, 1)
	go func() { _, err := index.Prepare(t.Context(), m); finished <- err }()
	<-started
	if _, _, err := s.DeleteAgent(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-finished; !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale extraction did not detect deletion: %v", err)
	}
	if _, _, err := s.Attachment(t.Context(), a.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted attachment returned: %v", err)
	}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &m); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("late event recreated deleted conversation: %v", err)
	}
}
