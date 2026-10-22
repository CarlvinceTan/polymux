package store

import (
	"errors"
	"sync"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestChoiceAnswer(t *testing.T) {
	for _, option := range []string{"one", "_text", FocusDismissed} {
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
			if option == FocusDismissed {
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
