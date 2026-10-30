package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func (s *Store) CompleteCredential(ctx context.Context, thread, id string, skip bool, sealed []byte) (contract.Message, error) {
	var card contract.Message
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return card, err
	}
	defer tx.Rollback()
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=? AND thread_id=? AND role='assistant'`, id, thread).Scan(&data); err != nil {
		return card, ErrChoice
	}
	if err := json.Unmarshal([]byte(data), &card); err != nil {
		return card, err
	}
	if card.AgentID == nil || len(card.Parts) != 1 || card.Parts[0].Kind != contract.PartKindCredential || card.Parts[0].Credential == nil {
		return card, ErrChoice
	}
	request := card.Parts[0].Credential
	status := "saved"
	if skip {
		status = "skipped"
	}
	if request.Status != contract.CredentialRequestStatusPending {
		if request.Status == status {
			return card, nil
		}
		return card, ErrChoice
	}
	var member int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM thread_members WHERE thread_id=? AND agent_id=?`, thread, *card.AgentID).Scan(&member); err != nil {
		return card, err
	}
	if member != 1 {
		return card, ErrChoice
	}
	if !skip && (request.Ephemeral == nil || !*request.Ephemeral) {
		if len(sealed) == 0 {
			return card, ErrChoice
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credentials(owner,name,data) VALUES(?,?,?) ON CONFLICT(owner,name) DO UPDATE SET data=excluded.data`, *card.AgentID, request.Name, sealed); err != nil {
			return card, err
		}
	}
	request.Status = status
	if _, err := s.appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: contract.EventTypeMessageDone, AgentID: card.AgentID}, &card); err != nil {
		return card, err
	}
	if !skip {
		text := "Credential " + request.Name + " was saved for " + request.Origin + ". Continue the task using its named reference; never expose the value."
		message := contract.Message{ID: "credential-answer:" + id, ThreadID: thread, Role: contract.MessageRoleUser, Status: contract.MessageStatusDone, CreatedAt: Now(), Parts: []contract.Part{{Kind: contract.PartKindText, Text: &text}}, ReplyToID: &card.ID}
		if _, err := s.appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: contract.EventTypeMessageStart}, &message); err != nil {
			return card, err
		}
		if err := s.queueTx(ctx, tx, Work{Trigger: contract.RunTriggerMessage, Message: message, Routing: "explicit", Targets: []string{*card.AgentID}}, *card.AgentID); err != nil {
			return card, err
		}
	}
	return card, tx.Commit()
}

func (s *Store) CredentialCard(ctx context.Context, thread, id string) (contract.Message, error) {
	card, err := s.Message(ctx, id)
	if err != nil || card.ThreadID != thread || card.AgentID == nil || card.Role != contract.MessageRoleAssistant || len(card.Parts) != 1 || card.Parts[0].Kind != contract.PartKindCredential || card.Parts[0].Credential == nil {
		return card, errors.Join(err, ErrChoice)
	}
	return card, nil
}
