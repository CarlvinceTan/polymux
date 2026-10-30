package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestChoiceAnswer(t *testing.T) {
	for _, option := range []string{"one", "_text", ChoiceDismissed} {
		t.Run(option, func(t *testing.T) {
			s := runStore(t)
			allow := true
			card := contract.Message{ID: "question", ThreadID: "a", Role: "assistant", Status: "done", AgentID: ptr("a"), Parts: []contract.Part{{Kind: "choice", Question: ptr("Which approach?"), Options: []contract.ChoiceOption{{ID: "one", Label: "First"}, {ID: "two", Label: "Second"}}, AllowText: &allow}}}
			if _, err := s.PostMessage(t.Context(), card); err != nil {
				t.Fatal(err)
			}
			answer := contract.ChoiceAnswer{OptionID: option}
			if option == "_text" {
				answer.Text = ptr("My alternative")
			}
			if _, err := s.AnswerChoice(t.Context(), "b", card.ID, answer); !errors.Is(err, ErrChoice) {
				t.Fatal("answered another conversation's question", err)
			}
			var calls sync.WaitGroup
			for range 8 {
				calls.Go(func() {
					if _, err := s.AnswerChoice(t.Context(), "a", card.ID, answer); err != nil {
						t.Error(err)
					}
				})
			}
			calls.Wait()
			// Replaying the tool must retain the saved answer.
			replayed, err := s.PostMessage(t.Context(), card)
			if err != nil || replayed.Parts[0].AnswerID == nil || *replayed.Parts[0].AnswerID != option {
				t.Fatal("replayed question lost its answer", replayed, err)
			}
			if _, err := s.AnswerChoice(t.Context(), "a", card.ID, contract.ChoiceAnswer{OptionID: "two"}); !errors.Is(err, ErrChoice) {
				t.Fatal("changed an answered question", err)
			}
			var jobs, replies int
			if err := s.DB.QueryRow(`SELECT count(*) FROM jobs`).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.QueryRow(`SELECT count(*) FROM messages WHERE role='user'`).Scan(&replies); err != nil {
				t.Fatal(err)
			}
			want := 1
			if option == ChoiceDismissed {
				want = 0
			}
			if jobs != want || replies != want {
				t.Fatalf("answer queued more than once: jobs=%d replies=%d", jobs, replies)
			}
			if want == 1 {
				work, err := s.Work(t.Context(), "answer:"+card.ID)
				if err != nil || len(work.Targets) != 1 || work.Targets[0] != "a" || work.Message.Reply == nil || work.Message.Reply.Text != "Which approach?" {
					t.Fatal("answer lost its author or question context", work, err)
				}
			}
		})
	}
}

func TestRetiredFocusCardsCloseOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "A"}}); err != nil {
		t.Fatal(err)
	}
	card := contract.Message{ID: "focus:a", ThreadID: "a", Role: "assistant", Status: "done", Parts: []contract.Part{{Kind: "choice", Question: ptr("What do you want me on first?"), Options: []contract.ChoiceOption{{ID: "day", Label: "Day-to-day tasks"}}}}}
	if _, err := s.PostMessage(t.Context(), card); err != nil {
		t.Fatal(err)
	}
	s.Close()
	for range 2 {
		if s, err = Open(t.Context(), path); err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	closed, err := s.Message(t.Context(), "focus:a")
	if err != nil || closed.Parts[0].AnswerID == nil || *closed.Parts[0].AnswerID != ChoiceDismissed {
		t.Fatalf("card was not skipped: %+v %v", closed, err)
	}
	events, err := s.Events(t.Context(), "a", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	updates := 0
	for _, event := range events {
		if event.Message != nil && event.Message.ID == "focus:a" && event.Message.Parts[0].AnswerID != nil {
			updates++
		}
	}
	if updates != 1 {
		t.Fatalf("clients should see one skip, got %d", updates)
	}
}
