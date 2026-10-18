package store

import (
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestHistory(t *testing.T) {
	s := openTest(t)
	for _, item := range []struct{ id, thread, status string }{
		{"old", "a", "done"}, {"failed", "a", "error"}, {"stream", "a", "streaming"},
		{"new", "a", "done"}, {"private", "b", "done"},
	} {
		text := "orange " + item.id
		m := contract.Message{ID: item.id, ThreadID: item.thread, Role: "assistant", Status: item.status,
			Parts: []contract.Part{{Kind: "text", Text: &text}}, CreatedAt: Now()}
		if _, err := s.Append(t.Context(), contract.Event{ConversationID: item.thread, Type: "message.start"}, &m); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.ExecContext(t.Context(), `INSERT INTO messages_fts(message_id,thread_id,body) VALUES(?,?,?)`, item.id, item.thread, text); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.History(t.Context(), "a", "a", "", 0, 1)
	if err != nil || len(items) != 1 || items[0].ID != "new" {
		t.Fatal("latest completed page", items, err)
	}
	older, err := s.History(t.Context(), "a", "a", "", items[0].Seq, 1)
	if err != nil || len(older) != 1 || older[0].ID != "old" {
		t.Fatal("page skipped or duplicated evidence", older, err)
	}
	items, err = s.History(t.Context(), "a", "a", `"orange"`, 0, 24)
	if err != nil || len(items) != 2 {
		t.Fatal("lexical query included incomplete evidence", items, err)
	}
	items, err = s.History(t.Context(), "a", "b", "", 0, 24)
	if err != nil || len(items) != 0 {
		t.Fatal("cross-conversation disclosure", items, err)
	}
	if _, err := s.DB.ExecContext(t.Context(), `DELETE FROM thread_members WHERE thread_id='a' AND agent_id='a'`); err != nil {
		t.Fatal(err)
	}
	items, err = s.History(t.Context(), "a", "a", "", 0, 24)
	if err != nil || len(items) != 0 {
		t.Fatal("revoked membership retained access", items, err)
	}
}
