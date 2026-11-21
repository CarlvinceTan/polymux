package store

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/packages/search"
	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type historyEmbed func(context.Context, search.Input) ([]float32, string, error)

func (f historyEmbed) Embed(ctx context.Context, input search.Input) ([]float32, string, error) {
	return f(ctx, input)
}

func TestSearchHistoryScope(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	const model = "test@2"
	add := func(id, thread, status, role, text string, v []float32) int64 {
		t.Helper()
		m := contract.Message{ID: id, ThreadID: thread, Role: role, Status: status,
			Parts: []contract.Part{{Kind: "text", Text: &text}}}
		event, err := s.Append(ctx, contract.Event{ConversationID: thread, Type: "message.start"}, &m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO messages_fts(message_id,thread_id,body) VALUES(?,?,?)`, id, thread, text); err != nil {
			t.Fatal(err)
		}
		blob, err := vec.SerializeFloat32(v)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO media_vectors(message_id,thread_id,embedding,model) VALUES(?,?,?,?)`, id, thread, blob, model); err != nil {
			t.Fatal(err)
		}
		return event.Seq
	}
	add("keyword", "a", "done", "user", "Anyone up for a swim?", []float32{0, 1})
	add("paraphrase", "a", "done", "assistant", "The pool opens at seven.", []float32{0.9, 0.1})
	for i := range historyCandidates + 1 {
		strong := strings.Repeat("swim ", 10)
		add(fmt.Sprint("private", i), "b", "done", "user", strong, []float32{1, 0})
		add(fmt.Sprint("failed", i), "a", "error", "assistant", strong, []float32{1, 0})
		add(fmt.Sprint("system", i), "a", "done", "system", strong, []float32{1, 0})
	}
	before := add("cutoff", "a", "done", "user", "swim swim swim", []float32{1, 0})
	for i := range historyCandidates + 1 {
		add(fmt.Sprint("future", i), "a", "done", "user", "swim swim swim", []float32{1, 0})
	}
	embed := historyEmbed(func(context.Context, search.Input) ([]float32, string, error) {
		return []float32{1, 0}, model, nil
	})
	messages, err := s.SearchHistory(ctx, "a", "a", "swim", before, 24, embed)
	if err != nil || len(messages) != 2 {
		t.Fatalf("expected both eligible matches despite stronger ineligible hits: %v %v", messages, err)
	}
	for _, hit := range messages {
		switch hit.ID {
		case "keyword":
			if !strings.Contains(hit.Snippet, "\x01swim\x02") {
				t.Fatalf("lost the keyword excerpt: %q", hit.Snippet)
			}
		case "paraphrase":
			if Text(hit.Message) != "The pool opens at seven." {
				t.Fatalf("wrong semantic evidence: %q", Text(hit.Message))
			}
		default:
			t.Fatalf("returned ineligible message %q", hit.ID)
		}
	}
	// Recheck membership after ranking, not just when gathering candidates.
	corpus := historySearch{store: s, thread: "a", agent: "a", before: before, semantic: ctx, embedder: embed}
	hits, _, err := (search.Engine{Embedder: corpus}).Search(ctx, "swim", corpus)
	if err != nil || len(hits) != 2 {
		t.Fatalf("expected two eligible candidates before revocation: %v %v", hits, err)
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM thread_members WHERE thread_id='a' AND agent_id='a'`); err != nil {
		t.Fatal(err)
	}
	messages, err = corpus.messages(ctx, hits, 24)
	if err != nil || len(messages) != 0 {
		t.Fatalf("materialization retained revoked access: %v %v", messages, err)
	}
}
