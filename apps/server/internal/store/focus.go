package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// FocusQuestion seeds the first-run card. Producing it spends no model tokens.
const FocusQuestion = "What do you want me on first?"

// FocusDismissed marks a card the user skipped instead of choosing.
const FocusDismissed = "_dismissed"

// ErrFocus reports a card that is missing, already answered, or given an unknown option.
var ErrFocus = errors.New("first-run card is not answerable")

// FocusOptions are the first-run choices offered for a new agent's first task.
var FocusOptions = []contract.ChoiceOption{
	{ID: "day", Label: "Day-to-day tasks"},
	{ID: "research", Label: "Research and writing"},
	{ID: "code", Label: "Code and projects"},
	{ID: "everything", Label: "A bit of everything"},
}

func focusID(agent string) string { return "focus:" + agent }

// FocusCard posts the first-run card once, and only while the conversation has no user turn.
// A deterministic id and a single-connection store keep concurrent callers from duplicating it.
func (s *Store) FocusCard(ctx context.Context, agent string) (contract.Message, bool, error) {
	var m contract.Message
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return m, false, err
	}
	defer tx.Rollback()
	var users, existing int
	err = tx.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM messages WHERE thread_id=? AND role='user'),
		(SELECT count(*) FROM messages WHERE id=?)`, agent, focusID(agent)).Scan(&users, &existing)
	if err != nil {
		return m, false, err
	}
	if users > 0 || existing > 0 {
		return m, false, nil
	}
	question := FocusQuestion
	m = contract.Message{
		ID: focusID(agent), ThreadID: agent, Role: "assistant", Status: "done",
		Parts:     []contract.Part{{Kind: "choice", Question: &question, Options: FocusOptions}},
		CreatedAt: Now(),
	}
	if _, err := appendTx(ctx, tx, contract.Event{ConversationID: agent, Type: "message.start"}, &m); err != nil {
		return m, false, err
	}
	if err := tx.Commit(); err != nil {
		return m, false, err
	}
	return m, true, nil
}

// AnswerFocus records the chosen option and posts a deterministic acknowledgment.
// A dismissed card is marked without a reply, so the conversation stays quiet.
func (s *Store) AnswerFocus(ctx context.Context, agent, option string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=?`, focusID(agent)).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFocus
		}
		return err
	}
	var card contract.Message
	if err := json.Unmarshal([]byte(data), &card); err != nil {
		return err
	}
	index := -1
	for i, part := range card.Parts {
		if part.Kind == "choice" {
			index = i
			break
		}
	}
	if index < 0 || card.Parts[index].AnswerID != nil {
		return ErrFocus
	}
	label := ""
	if option != FocusDismissed {
		for _, candidate := range card.Parts[index].Options {
			if candidate.ID == option {
				label = candidate.Label
				break
			}
		}
		if label == "" {
			return ErrFocus
		}
	}
	card.Parts[index].AnswerID = &option
	if _, err := appendTx(ctx, tx, contract.Event{ConversationID: agent, Type: "message.start"}, &card); err != nil {
		return err
	}
	if option != FocusDismissed {
		text := "Got it — " + label + ". What's the first thing you want me to do?"
		ack := contract.Message{
			ID: "focus:" + agent + ":ack", ThreadID: agent, Role: "assistant", Status: "done",
			Parts:     []contract.Part{{Kind: "text", Text: &text}},
			CreatedAt: Now(),
		}
		if _, err := appendTx(ctx, tx, contract.Event{ConversationID: agent, Type: "message.start"}, &ack); err != nil {
			return err
		}
	}
	return tx.Commit()
}
