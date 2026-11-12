package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

var ErrApproval = errors.New("approval is not answerable")

// SettleApproval closes a pending approval card with the person's answer.
func (s *Store) SettleApproval(ctx context.Context, thread, id, status, answerID string) (contract.Message, error) {
	var card contract.Message
	if status != contract.ApprovalStatusAllowed && status != contract.ApprovalStatusDenied && status != contract.ApprovalStatusExpired {
		return card, ErrApproval
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return card, err
	}
	defer tx.Rollback()
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=? AND thread_id=? AND role='assistant'`, id, thread).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return card, ErrApproval
		}
		return card, err
	}
	if err := json.Unmarshal([]byte(data), &card); err != nil {
		return card, err
	}
	if card.AgentID == nil || len(card.Parts) != 1 || card.Parts[0].Kind != contract.PartKindApproval || card.Parts[0].Approval == nil {
		return card, ErrApproval
	}
	approval := card.Parts[0].Approval
	if approval.Status != contract.ApprovalStatusPending {
		if approval.Status == status {
			return card, nil
		}
		return card, ErrApproval
	}
	approval.Status = status
	if answerID != "" {
		approval.AnswerID = &answerID
	}
	if _, err := s.appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: contract.EventTypeMessageDone, AgentID: card.AgentID}, &card); err != nil {
		return card, err
	}
	return card, tx.Commit()
}

// ApprovalNotice tells the person a gate is waiting, including devices that aren't in the chat.
func (s *Store) ApprovalNotice(ctx context.Context, card contract.Message) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	agent := ""
	if card.AgentID != nil {
		agent = *card.AgentID
	}
	title := "Polymux"
	if agent != "" {
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT json_extract(data,'$.name') FROM agents WHERE id=?`, agent).Scan(&name); err == nil && name != "" {
			title = name
		}
	}
	notice := contract.Notice{ConversationID: card.ThreadID, MessageID: card.ID, AgentID: agent, Title: title, Body: "Needs your approval", Outcome: "waiting_input", CreatedAt: Now()}
	if err := saveNotice(ctx, tx, notice, card.ID, "approval:"+card.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// ApprovalCard reads one approval card and checks that it belongs to the chat.
func (s *Store) ApprovalCard(ctx context.Context, thread, id string) (contract.Message, error) {
	card, err := s.Message(ctx, id)
	if err != nil || card.ThreadID != thread || card.AgentID == nil || card.Role != contract.MessageRoleAssistant || len(card.Parts) != 1 || card.Parts[0].Kind != contract.PartKindApproval || card.Parts[0].Approval == nil {
		return card, errors.Join(err, ErrApproval)
	}
	return card, nil
}

// expireApprovals closes cards left waiting by a stop the tool never came back from.
func (s *Store) expireApprovals(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, thread_id FROM messages WHERE json_extract(data,'$.parts[0].kind')='approval' AND json_extract(data,'$.parts[0].approval.status')='pending'`)
	if err != nil {
		return err
	}
	type card struct{ id, thread string }
	var cards []card
	for rows.Next() {
		var item card
		if err := rows.Scan(&item.id, &item.thread); err != nil {
			rows.Close()
			return err
		}
		cards = append(cards, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range cards {
		if _, err := s.SettleApproval(ctx, item.thread, item.id, contract.ApprovalStatusExpired, ""); err != nil && !errors.Is(err, ErrApproval) {
			return err
		}
	}
	return nil
}
