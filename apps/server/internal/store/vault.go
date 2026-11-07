package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func (s *Store) VaultCard(ctx context.Context, id string) (contract.Message, error) {
	card, err := s.Message(ctx, id)
	if err != nil {
		return card, err
	}
	if !validVaultCard(card) {
		return card, ErrChoice
	}
	member, err := s.Member(ctx, *card.AgentID, card.ThreadID)
	if err != nil {
		return card, err
	}
	if !member {
		return card, ErrChoice
	}
	return card, nil
}

// A private card can sit in any conversation the bot belongs to; membership is checked by callers.
func validVaultCard(card contract.Message) bool {
	return card.AgentID != nil && card.Role == "assistant" &&
		len(card.Parts) == 1 && card.Parts[0].Kind == contract.PartKindVault && card.Parts[0].Vault != nil
}

// FinishVault writes only an acknowledgement, never decrypted fields or imported file contents.
func (s *Store) FinishVault(ctx context.Context, tx *sql.Tx, id string) error {
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=?`, id).Scan(&data); err != nil {
		return err
	}
	var card contract.Message
	if json.Unmarshal([]byte(data), &card) != nil || !validVaultCard(card) {
		return ErrChoice
	}
	ref := card.Parts[0].Vault
	if ref.Status == "done" {
		return nil
	}
	var member int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM thread_members WHERE thread_id=? AND agent_id=?`, card.ThreadID, *card.AgentID).Scan(&member); err != nil {
		return err
	}
	if member != 1 {
		return ErrChoice
	}
	ref.Status = "done"
	if _, err := s.appendTx(ctx, tx, contract.Event{ConversationID: card.ThreadID, Type: contract.EventTypeMessageDone, AgentID: card.AgentID}, &card); err != nil {
		return err
	}
	text := "Private vault action completed (" + ref.Action + "). Continue using vault references; never request or inspect secret values."
	if ref.ItemID != "" {
		text += " Item: " + ref.ItemID
	}
	message := contract.Message{ID: "vault-answer:" + id, ThreadID: card.ThreadID, Role: "user", Status: "done", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}, ReplyToID: &card.ID}
	if _, err := s.appendTx(ctx, tx, contract.Event{ConversationID: card.ThreadID, Type: contract.EventTypeMessageStart}, &message); err != nil {
		return err
	}
	return s.queueTx(ctx, tx, Work{Trigger: contract.RunTriggerMessage, Message: message, Routing: "explicit", Targets: []string{*card.AgentID}}, *card.AgentID)
}
