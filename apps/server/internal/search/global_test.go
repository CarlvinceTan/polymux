package search

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func globalIndex(t *testing.T) *Index {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	agents := []contract.Agent{{ID: "a", Name: "Orchard", Role: "orange specialist"}, {ID: "b", Name: "Garage", Role: "bicycle specialist"}}
	if err := db.Register(t.Context(), agents); err != nil {
		t.Fatal(err)
	}
	return &Index{Store: db, Embedder: testEmbedding{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func indexMessage(t *testing.T, index *Index, id, thread, body string, attachment *contract.Attachment) {
	t.Helper()
	parts := []contract.Part{{Kind: "text", Text: &body}}
	if attachment != nil {
		if err := index.Store.SaveAttachment(t.Context(), *attachment, filepath.Join(t.TempDir(), attachment.Name)); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, contract.Part{Kind: "attachment", Attachment: attachment})
	}
	message := contract.Message{ID: id, ThreadID: thread, Role: "user", Status: "done", CreatedAt: store.Now(), Parts: parts}
	if _, err := index.Store.Append(t.Context(), contract.Event{ConversationID: thread, Type: "message.done"}, &message); err != nil {
		t.Fatal(err)
	}
	if err := index.Ingest(t.Context(), message); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalSearch(t *testing.T) {
	index := globalIndex(t)
	indexMessage(t, index, "one", "a", "orange balloon", nil)
	indexMessage(t, index, "two", "b", "orange juice", nil)
	hits, semantic, err := index.Global(t.Context(), "fruit", "messages", "")
	if err != nil || !semantic || len(hits) != 2 {
		t.Fatalf("results=%+v semantic=%v err=%v", hits, semantic, err)
	}
	threads := map[string]bool{}
	for _, hit := range hits {
		threads[hit.ThreadID] = true
		if hit.MessageID == nil || hit.Message == nil || hit.Kind != "messages" {
			t.Fatalf("missing target: %+v", hit)
		}
	}
	if !threads["a"] || !threads["b"] {
		t.Fatalf("did not cross conversations: %+v", hits)
	}
	index.Embedder = nil
	hits, semantic, err = index.Global(t.Context(), "juice", "all", "")
	if err != nil || semantic || len(hits) != 1 || hits[0].ThreadID != "b" {
		t.Fatalf("fallback=%+v semantic=%v err=%v", hits, semantic, err)
	}
}

// A search from one chat's profile only finds what was said and shared there.
func TestGlobalSearchScopedToChat(t *testing.T) {
	index := globalIndex(t)
	indexMessage(t, index, "one", "a", "orange balloon", nil)
	indexMessage(t, index, "two", "b", "orange juice", nil)
	hits, _, err := index.Global(t.Context(), "orange", "all", "a")
	if err != nil || len(hits) != 1 || hits[0].ThreadID != "a" || hits[0].Kind != "messages" {
		t.Fatalf("scoped results=%+v err=%v", hits, err)
	}
}

func TestGlobalPrefixes(t *testing.T) {
	index := globalIndex(t)
	indexMessage(
		t,
		index,
		"message",
		"a",
		"Passport renewal checklist",
		nil,
	)
	text := "Vaccination requirements"
	file := contract.Attachment{
		ID: "file", Name: "itinerary.pdf", MimeType: "application/pdf", ExtractedText: &text, IndexingState: "ready",
	}
	image := contract.Attachment{ID: "image", Name: "sunset.png", MimeType: "image/png", IndexingState: "ready"}
	indexMessage(
		t,
		index,
		"document",
		"b",
		"Shared document",
		&file,
	)
	indexMessage(
		t,
		index,
		"photo",
		"b",
		"Photo attached",
		&image,
	)
	index.Embedder = nil
	for _, tt := range []struct{ name, kind, query, id string }{
		{name: "early prefix", kind: "messages", query: "pa", id: "message/message"},
		{name: "unfinished word", kind: "messages", query: "pass", id: "message/message"},
		{name: "complete word", kind: "messages", query: "passport", id: "message/message"},
		{name: "case insensitive", kind: "messages", query: "PASS", id: "message/message"},
		{name: "completed words", kind: "messages", query: "a pass", id: "message/message"},
		{name: "filename", kind: "files", query: "iti", id: "attachment/file"},
		{name: "file content", kind: "files", query: "vacc", id: "attachment/file"},
		{name: "media filename", kind: "media", query: "sun", id: "attachment/image"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hits, semantic, err := index.Global(t.Context(), tt.query, tt.kind, "")
			if err != nil || semantic || len(hits) != 1 || hits[0].ID != tt.id {
				t.Fatalf("results=%+v semantic=%v err=%v", hits, semantic, err)
			}
		})
	}
	hits, _, err := index.Search(t.Context(), "b", "pass")
	if err != nil || len(hits) != 0 {
		t.Fatalf("prefix escaped conversation scope: %+v err=%v", hits, err)
	}
}

func TestGlobalTypesBeforeLimit(t *testing.T) {
	index := globalIndex(t)
	for n := range 85 {
		indexMessage(t, index, fmt.Sprintf("text%03d", n), "a", "orange", nil)
	}
	image := contract.Attachment{ID: "image", Name: "orange.png", MimeType: "image/png", IndexingState: "ready"}
	file := contract.Attachment{ID: "file", Name: "orange.pdf", MimeType: "application/pdf", IndexingState: "ready"}
	indexMessage(t, index, "zz-image", "b", "orange", &image)
	indexMessage(t, index, "zz-file", "b", "orange", &file)
	for _, tt := range []struct{ name, id string }{{name: "media", id: "image"}, {name: "files", id: "file"}} {
		t.Run(tt.name, func(t *testing.T) {
			hits, _, err := index.Global(t.Context(), "orange", tt.name, "")
			if err != nil || len(hits) != 1 || hits[0].Attachment == nil || hits[0].Attachment.ID != tt.id {
				t.Fatalf("filtered=%+v err=%v", hits, err)
			}
		})
	}
	index.Embedder = nil
	hits, _, err := index.Global(t.Context(), "orange.pdf", "files", "")
	if err != nil || len(hits) != 1 || hits[0].Title != "orange.pdf" {
		t.Fatalf("filename=%+v err=%v", hits, err)
	}
}

func TestGlobalMetadata(t *testing.T) {
	index := globalIndex(t)
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"bots", "conversations"} {
		hits, semantic, err := index.Global(t.Context(), "fruit", kind, "")
		if err != nil || !semantic || len(hits) != 2 || hits[0].ThreadID != "a" || hits[0].Kind != kind {
			t.Fatalf("%s=%+v semantic=%v err=%v", kind, hits, semantic, err)
		}
	}
	if err := index.Store.Register(t.Context(), []contract.Agent{{ID: "a", Name: "Renamed", Role: "new role"}}); err != nil {
		t.Fatal(err)
	}
	index.Embedder = nil
	hits, _, err := index.Global(t.Context(), "Renamed", "bots", "")
	if err != nil || len(hits) != 1 || hits[0].Title != "Renamed" {
		t.Fatalf("renamed=%+v err=%v", hits, err)
	}
	if err := index.Store.DeleteAgent(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	hits, _, err = index.Global(t.Context(), "Renamed", "all", "")
	if err != nil || len(hits) != 0 {
		t.Fatalf("deleted=%+v err=%v", hits, err)
	}
}

func TestModelReplacement(t *testing.T) {
	index := globalIndex(t)
	old := &fixedEmbedding{model: "old@3"}
	index.Embedder = old
	indexMessage(t, index, "old-message", "a", "a previous flight decision", nil)
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	replacement := &fixedEmbedding{model: "replacement@3"}
	index.Embedder = replacement
	if err := index.Reindex(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		kind string
		want int
	}{{kind: "messages", want: 1}, {kind: "bots", want: 2}, {kind: "conversations", want: 2}} {
		hits, semantic, err := index.Global(t.Context(), "transport reservation", tt.kind, "")
		if err != nil || !semantic || len(hits) != tt.want {
			t.Fatalf("%s results=%+v semantic=%v err=%v", tt.kind, hits, semantic, err)
		}
	}
	before := replacement.calls
	if err := index.Reindex(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if replacement.calls != before+1 {
		t.Fatal("unchanged model unnecessarily re-embedded existing records")
	}
}

func TestReindexRecovery(t *testing.T) {
	index := globalIndex(t)
	index.Embedder = &fixedEmbedding{model: "old@3"}
	indexMessage(t, index, "old-message", "a", "a previous flight decision", nil)
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	unavailable := errors.New("provider temporarily unavailable")
	replacement := &fixedEmbedding{model: "replacement@3", err: unavailable}
	index.Embedder = replacement
	if err := index.Reindex(t.Context()); !errors.Is(err, unavailable) {
		t.Fatal("missing startup probe error", err)
	}
	replacement.err = nil
	for range 2 {
		if err := index.Batch(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	hits, semantic, err := index.Global(t.Context(), "transport reservation", "messages", "")
	if err != nil || !semantic || len(hits) != 1 {
		t.Fatalf("recovered results=%+v semantic=%v err=%v", hits, semantic, err)
	}
	before := replacement.calls
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if replacement.calls != before {
		t.Fatal("resolved model was probed again")
	}
}

func TestIngestRecovery(t *testing.T) {
	index := globalIndex(t)
	embedder := &fixedEmbedding{model: "active@3"}
	index.Embedder = embedder
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	embedder.err = errors.New("provider temporarily unavailable")
	body := "a previous flight decision"
	message := contract.Message{
		ID: "during-outage", ThreadID: "a", Role: "user", Status: "done", CreatedAt: store.Now(),
		Parts: []contract.Part{{Kind: "text", Text: &body}},
	}
	if _, err := index.Store.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &message); err != nil {
		t.Fatal(err)
	}
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	hits, semantic, err := index.Global(t.Context(), "flight", "messages", "")
	if err != nil || semantic || len(hits) != 1 {
		t.Fatalf("outage lost lexical history=%+v semantic=%v err=%v", hits, semantic, err)
	}
	embedder.err = nil
	before := embedder.calls
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if embedder.calls != before {
		t.Fatal("failed embedding retried before its backoff elapsed")
	}
	index.retryAt = time.Time{}
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	hits, semantic, err = index.Global(t.Context(), "transport reservation", "messages", "")
	if err != nil || !semantic || len(hits) != 1 {
		t.Fatalf("recovered results=%+v semantic=%v err=%v", hits, semantic, err)
	}
	var state string
	if err := index.Store.DB.QueryRowContext(t.Context(), `SELECT indexing_state FROM messages WHERE id=?`, message.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "ready" {
		t.Fatal("recovered message did not become ready", state)
	}
}

func TestRetryPrioritizesPending(t *testing.T) {
	index := globalIndex(t)
	index.Embedder = &fixedEmbedding{model: "active@3"}
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	for n := range 16 {
		text := "Earlier conversation"
		message := contract.Message{
			ID: fmt.Sprintf("old-%d", n), ThreadID: "a", Role: "user", Status: "done", CreatedAt: store.Now(),
			Parts: []contract.Part{{Kind: "text", Text: &text}},
		}
		if _, err := index.Store.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &message); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := index.Store.DB.ExecContext(t.Context(), `UPDATE messages SET indexing_state='error'`); err != nil {
		t.Fatal(err)
	}
	text := "Current conversation"
	message := contract.Message{
		ID: "fresh", ThreadID: "a", Role: "user", Status: "done", CreatedAt: store.Now(),
		Parts: []contract.Part{{Kind: "text", Text: &text}},
	}
	if _, err := index.Store.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &message); err != nil {
		t.Fatal(err)
	}
	index.retryAt = time.Time{}
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := index.Store.DB.QueryRowContext(t.Context(), `SELECT indexing_state FROM messages WHERE id='fresh'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "ready" {
		t.Fatal("old failures blocked fresh history ingestion", state)
	}
	var failed int
	if err := index.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM messages WHERE indexing_state='error'`).Scan(&failed); err != nil || failed != 16 {
		t.Fatal("retried errors before pending history drained", failed, err)
	}
	index.retryAt = time.Time{}
	if err := index.Batch(t.Context()); err != nil {
		t.Fatal(err)
	}
	var ready int
	if err := index.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM messages WHERE indexing_state='ready' AND id<>'fresh'`).Scan(&ready); err != nil || ready != 8 {
		t.Fatal("failed history did not retry after pending queue drained", ready, err)
	}
}
