package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestAttachmentRollback(t *testing.T) {
	s := openTest(t)
	a := contract.Attachment{ID: "card", Kind: "html", Name: "Card.html", MimeType: "text/html", URL: "/attachments/card/content", IndexingState: "ready"}
	m := contract.Message{ID: "reply", ThreadID: "missing", Role: "assistant", Parts: []contract.Part{{Kind: "attachment", Attachment: &a}}, Status: "streaming", CreatedAt: Now()}
	if _, err := s.PublishAttachment(t.Context(), a, "/private/card", contract.Event{ConversationID: "missing", Type: "message.attachment"}, &m); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing conversation error: %v", err)
	}
	if _, _, err := s.Attachment(t.Context(), a.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("attachment survived failed publication: %v", err)
	}
	if _, err := s.Message(t.Context(), m.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("message survived failed publication: %v", err)
	}
	thread, err := s.Thread(t.Context(), "a")
	if err != nil || thread.LastSeq != 0 {
		t.Fatalf("unrelated cursor changed: %+v %v", thread, err)
	}
}
