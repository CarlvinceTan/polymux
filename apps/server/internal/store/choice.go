package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// ChoiceDismissed marks a question card the user skipped instead of answering.
const ChoiceDismissed = "_dismissed"

var ErrChoice = errors.New("question is not answerable")

// PostMessage makes a tool's visible message idempotent across retries.
func (s *Store) PostMessage(ctx context.Context, message contract.Message) (contract.Message, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return message, err
	}
	defer tx.Rollback()
	var data string
	err = tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=?`, message.ID).Scan(&data)
	if err == nil {
		var old contract.Message
		if err := json.Unmarshal([]byte(data), &old); err != nil {
			return message, err
		}
		if old.ThreadID != message.ThreadID || choiceText(old.AgentID) != choiceText(message.AgentID) {
			return message, ErrConflict
		}
		return old, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return message, err
	}
	if _, err := s.appendTx(ctx, tx, contract.Event{ConversationID: message.ThreadID, Type: contract.EventTypeMessageStart, AgentID: message.AgentID, RunID: message.RunID}, &message); err != nil {
		return message, err
	}
	return message, tx.Commit()
}

func choiceText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// AnswerChoice commits the answer and its continuation together.
func (s *Store) AnswerChoice(ctx context.Context, thread, id string, answer contract.ChoiceAnswer) (contract.Message, error) {
	var card contract.Message
	text := strings.TrimSpace(choiceText(answer.Text))
	if len(text) > 8192 || len(answer.OptionID) > 64 {
		return card, ErrChoice
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return card, err
	}
	defer tx.Rollback()
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=? AND thread_id=? AND role='assistant'`, id, thread).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return card, ErrChoice
		}
		return card, err
	}
	if err := json.Unmarshal([]byte(data), &card); err != nil {
		return card, err
	}
	if card.AgentID == nil || len(card.Parts) != 1 || card.Parts[0].Kind != contract.PartKindChoice {
		return card, ErrChoice
	}
	part := &card.Parts[0]
	if part.AnswerID != nil {
		if *part.AnswerID == answer.OptionID && choiceText(part.AnswerText) == text {
			return card, nil
		}
		return card, ErrChoice
	}
	label := ""
	switch answer.OptionID {
	case ChoiceDismissed:
		if text != "" {
			return card, ErrChoice
		}
	case "_text":
		if part.AllowText == nil || !*part.AllowText || text == "" {
			return card, ErrChoice
		}
		label = text
	default:
		if text != "" {
			return card, ErrChoice
		}
		for _, option := range part.Options {
			if option.ID == answer.OptionID {
				label = option.Label
				break
			}
		}
		if label == "" {
			return card, ErrChoice
		}
	}
	part.AnswerID = &answer.OptionID
	if text != "" {
		part.AnswerText = &text
	}
	if _, err := s.appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: contract.EventTypeMessageDone, AgentID: card.AgentID}, &card); err != nil {
		return card, err
	}
	if answer.OptionID != ChoiceDismissed {
		var member int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM thread_members WHERE thread_id=? AND agent_id=?`, thread, *card.AgentID).Scan(&member); err != nil {
			return card, err
		}
		if member != 1 {
			return card, ErrChoice
		}
		message := contract.Message{ID: "answer:" + id, ThreadID: thread, Role: contract.MessageRoleUser, Status: contract.MessageStatusDone, CreatedAt: Now(), Parts: []contract.Part{{Kind: contract.PartKindText, Text: &label}}, ReplyToID: &card.ID,
			Reply: &contract.ReplyContext{MessageID: card.ID, Role: contract.ReplyContextRoleAssistant, Text: choiceText(part.Question), AgentID: card.AgentID}}
		if _, err := s.appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: contract.EventTypeMessageStart}, &message); err != nil {
			return card, err
		}
		if err := s.queueTx(ctx, tx, Work{Trigger: contract.RunTriggerMessage, Message: message, Routing: "explicit", Targets: []string{*card.AgentID}}, *card.AgentID); err != nil {
			return card, err
		}
	}
	return card, tx.Commit()
}

// closeFocusCards marks the retired first-run cards that were never answered as skipped,
// so every client shows them settled instead of offering choices nothing can accept.
func (s *Store) closeFocusCards(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT data FROM messages WHERE id LIKE 'focus:%' AND json_extract(data,'$.parts[0].kind')='choice' AND json_extract(data,'$.parts[0].answerID') IS NULL`)
	if err != nil {
		return err
	}
	var cards []contract.Message
	for rows.Next() {
		var data string
		var card contract.Message
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(data), &card); err != nil {
			rows.Close()
			return err
		}
		cards = append(cards, card)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, card := range cards {
		skipped := ChoiceDismissed
		card.Parts[0].AnswerID = &skipped
		if _, err := s.appendTx(ctx, tx, contract.Event{ConversationID: card.ThreadID, Type: contract.EventTypeMessageStart, MessageID: &card.ID}, &card); err != nil {
			return err
		}
	}
	return tx.Commit()
}
