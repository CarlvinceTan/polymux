package search

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	engine "github.com/CarlvinceTan/polymux/packages/search"
)

func TestExtractionRetry(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Register(t.Context(), []contract.Agent{{ID: "audit"}}); err != nil {
		t.Fatal(err)
	}
	a := contract.Attachment{ID: "audit-file", Kind: "file", MimeType: "application/pdf", IndexingState: "pending"}
	if err := db.SaveAttachment(t.Context(), a, filepath.Join(t.TempDir(), "synthetic.pdf")); err != nil {
		t.Fatal(err)
	}
	m := contract.Message{ID: "audit-message", ThreadID: "audit", Role: "user", Status: "done", Parts: []contract.Part{{Kind: "attachment", Attachment: &a}}}
	if _, err := db.Append(t.Context(), contract.Event{ConversationID: "audit", Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	calls := 0
	index := &Index{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Extractor: engine.ExtractorFunc(func(context.Context, engine.Asset) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("synthetic transient failure")
		}
		return "Recovered attachment text", nil
	})}
	if _, err := index.Prepare(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Prepare(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	got, _, err := db.Attachment(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || got.IndexingState != "ready" {
		t.Fatalf("extractor calls=%d attachment state=%s; transient failure was never retried", calls, got.IndexingState)
	}
}
