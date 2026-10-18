package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestFocusCardPostsOnceForAnUntouchedChat(t *testing.T) {
	s := runStore(t)
	card, posted, err := s.FocusCard(t.Context(), "a")
	if err != nil || !posted {
		t.Fatalf("first card %+v posted=%v err=%v", card, posted, err)
	}
	if card.Role != "assistant" || card.Status != "done" || len(card.Parts) != 1 {
		t.Fatalf("unexpected card shape %+v", card)
	}
	part := card.Parts[0]
	if part.Kind != "choice" || part.Question == nil || *part.Question != FocusQuestion || len(part.Options) != len(FocusOptions) || part.AnswerID != nil {
		t.Fatalf("unexpected card part %+v", part)
	}
	if _, posted, err = s.FocusCard(t.Context(), "a"); err != nil || posted {
		t.Fatal("card was posted twice", posted, err)
	}
	messages, err := s.Messages(t.Context(), "a", 0, 100)
	if err != nil || len(messages) != 1 {
		t.Fatalf("expected one message, got %d %v", len(messages), err)
	}
}

func TestFocusCardWaitsForAnUntouchedChat(t *testing.T) {
	s := runStore(t)
	text := "Help with my draft"
	if _, _, err := s.Send(t.Context(), "a", contract.SendRequest{ID: "first", Text: text, AttachmentIDs: []string{}}, []contract.Part{{Kind: "text", Text: &text}}, "a"); err != nil {
		t.Fatal(err)
	}
	if _, posted, err := s.FocusCard(t.Context(), "a"); err != nil || posted {
		t.Fatal("card posted after the user spoke", posted, err)
	}
	// Another conversation is unaffected by the first one's user turn.
	if _, posted, err := s.FocusCard(t.Context(), "b"); err != nil || !posted {
		t.Fatal("card withheld from an untouched chat", posted, err)
	}
}

func TestAnswerFocusRecordsTheChoiceAndAcknowledges(t *testing.T) {
	s := runStore(t)
	if _, _, err := s.FocusCard(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.AnswerFocus(t.Context(), "a", "research"); err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages(t.Context(), "a", 0, 100)
	if err != nil || len(messages) != 2 {
		t.Fatalf("expected the card and an acknowledgment, got %d %v", len(messages), err)
	}
	answered := messages[0].Parts[0]
	if answered.AnswerID == nil || *answered.AnswerID != "research" {
		t.Fatalf("answer not recorded %+v", answered)
	}
	ack := messages[1]
	if ack.Role != "assistant" || ack.Parts[0].Text == nil || !strings.Contains(*ack.Parts[0].Text, "Research and writing") {
		t.Fatalf("unexpected acknowledgment %+v", ack)
	}
	if err := s.AnswerFocus(t.Context(), "a", "code"); !errors.Is(err, ErrFocus) {
		t.Fatal("answered card accepted a second answer", err)
	}
}

func TestAnswerFocusRejectsUnknownOptionsAndMissingCards(t *testing.T) {
	s := runStore(t)
	if err := s.AnswerFocus(t.Context(), "a", "day"); !errors.Is(err, ErrFocus) {
		t.Fatal("answer accepted without a card", err)
	}
	if _, _, err := s.FocusCard(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.AnswerFocus(t.Context(), "a", "invented"); !errors.Is(err, ErrFocus) {
		t.Fatal("unknown option accepted", err)
	}
	if messages, err := s.Messages(t.Context(), "a", 0, 100); err != nil || len(messages) != 1 {
		t.Fatalf("rejected answer still wrote %d %v", len(messages), err)
	}
}

func TestDismissedFocusCardStaysQuiet(t *testing.T) {
	s := runStore(t)
	if _, _, err := s.FocusCard(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.AnswerFocus(t.Context(), "a", FocusDismissed); err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages(t.Context(), "a", 0, 100)
	if err != nil || len(messages) != 1 {
		t.Fatalf("dismissal posted a reply: %d %v", len(messages), err)
	}
	if answer := messages[0].Parts[0].AnswerID; answer == nil || *answer != FocusDismissed {
		t.Fatalf("dismissal not recorded %+v", messages[0].Parts[0])
	}
	if _, posted, err := s.FocusCard(t.Context(), "a"); err != nil || posted {
		t.Fatal("dismissed card was re-posted", posted, err)
	}
}
