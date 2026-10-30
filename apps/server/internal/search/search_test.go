package search

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	engine "github.com/CarlvinceTan/polymux/packages/search"
)

type testEmbedding struct{}

func (testEmbedding) Embed(_ context.Context, in engine.Input) ([]float32, string, error) {
	if strings.Contains(in.Text, "orange") || strings.Contains(in.Text, "fruit") {
		return []float32{1, 0, 0}, "test", nil
	}
	return []float32{0, 1, 0}, "test", nil
}

func TestIndexAdmission(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Register(t.Context(), []contract.Agent{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	message := contract.Message{ID: "pending", ThreadID: "a", Role: "user", Status: "done",
		CreatedAt: store.Now(), Parts: []contract.Part{{Kind: "text", Text: new("Index after the update")}}}
	if _, err := db.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &message); err != nil {
		t.Fatal(err)
	}
	index := &Index{Store: db, Embedder: testEmbedding{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	var open atomic.Bool
	attempted := make(chan bool, 8)
	finished := make(chan struct{}, 8)
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		index.Run(ctx, func() bool {
			allowed := open.Load()
			attempted <- allowed
			return allowed
		}, func() { finished <- struct{}{} })
	}()
	defer func() { cancel(); <-stopped }()
	select {
	case admitted := <-attempted:
		if admitted {
			t.Fatal("index admitted while paused")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("index did not attempt admission")
	}
	var state string
	if err := db.DB.QueryRowContext(t.Context(), `SELECT indexing_state FROM messages WHERE id='pending'`).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("paused index processed pending work: %s %v", state, err)
	}
	open.Store(true)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("index did not resume")
	}
	if err := db.DB.QueryRowContext(t.Context(), `SELECT indexing_state FROM messages WHERE id='pending'`).Scan(&state); err != nil || state != "ready" {
		t.Fatalf("resumed index did not finish: %s %v", state, err)
	}
}

func TestHybridScope(t *testing.T) {
	ctx := t.Context()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Register(ctx, []contract.Agent{{ID: "a"}, {ID: "b"}}); err != nil {
		t.Fatal(err)
	}
	i := &Index{Store: s, Embedder: testEmbedding{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, item := range []struct{ id, thread, text string }{{"m1", "a", "orange balloon"}, {"m2", "a", "blue bicycle"}, {"m3", "b", "fruit outside this conversation"}} {
		m := contract.Message{ID: item.id, ThreadID: item.thread, Role: "user", Parts: []contract.Part{{Kind: "text", Text: &item.text}}, Status: "done", CreatedAt: store.Now()}
		if _, err := s.Append(ctx, contract.Event{ConversationID: item.thread, Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
		if err := i.Ingest(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	hits, hybrid, err := i.Search(ctx, "a", "fruit")
	if err != nil || !hybrid || len(hits) != 2 || hits[0].Message.ID != "m1" {
		t.Fatalf("semantic rank %+v hybrid=%v err=%v", hits, hybrid, err)
	}
	for _, hit := range hits {
		if hit.Message.ThreadID != "a" {
			t.Fatal("cross-conversation search leak")
		}
	}
	i.Embedder = nil
	hits, hybrid, err = i.Search(ctx, "a", "bicycle")
	if err != nil || hybrid || len(hits) != 1 || hits[0].Message.ID != "m2" {
		t.Fatalf("lexical fallback %+v %v %v", hits, hybrid, err)
	}
}

func TestIndexUnavailableLogsOnce(t *testing.T) {
	ctx := t.Context()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.DB.ExecContext(ctx, "DROP TABLE messages_fts"); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(ctx, []contract.Agent{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	i := &Index{Store: s, Log: slog.New(slog.NewTextHandler(&log, nil))}
	for _, id := range []string{"m1", "m2"} {
		text := "hello " + id
		m := contract.Message{ID: id, ThreadID: "a", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}}, Status: "done", CreatedAt: store.Now()}
		if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
	}
	if err := i.Batch(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(log.String(), "search index unavailable"); got != 1 {
		t.Fatalf("unavailable log count = %d: %s", got, log.String())
	}
	if strings.Contains(log.String(), "index message") {
		t.Fatalf("logged per message: %s", log.String())
	}
}

func TestReindexQueuesStaleVectors(t *testing.T) {
	ctx := t.Context()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Register(ctx, []contract.Agent{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	for id, model := range map[string]string{"size": "m@512", "model": "old@768", "current": "m@768"} {
		body := id
		m := contract.Message{ID: id, ThreadID: "a", Role: "user", Status: "done", CreatedAt: store.Now(),
			Parts: []contract.Part{{Kind: "text", Text: &body}}}
		if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE messages SET indexing_state='ready' WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO media_vectors(message_id,thread_id,embedding,model) VALUES(?,?,?,?)`, id, "a", []byte{0}, model); err != nil {
			t.Fatal(err)
		}
	}
	index := &Index{Store: s, Embedder: &fixedEmbedding{model: "m@768"}}
	if err := index.Reindex(ctx); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"size": "pending", "model": "pending", "current": "ready"} {
		var got string
		if err := s.DB.QueryRowContext(ctx, `SELECT indexing_state FROM messages WHERE id=?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s is %s, want %s", id, got, want)
		}
	}
}

type fixedEmbedding struct {
	model string
	calls int
	err   error
}

func (e *fixedEmbedding) Embed(context.Context, engine.Input) ([]float32, string, error) {
	e.calls++
	return []float32{1, 0, 0}, e.model, e.err
}
