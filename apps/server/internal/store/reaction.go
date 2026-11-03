package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

var ErrReaction = errors.New("reaction is unavailable")

// React sets one actor's reaction without toggling it again on a retry.
func (s *Store) React(ctx context.Context, thread, id string, request contract.ReactionRequest, agentID *string) (contract.Message, error) {
	var m contract.Message
	if !validReaction(request.Emoji) || agentID != nil && *agentID == "" {
		return m, ErrReaction
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=? AND thread_id=? AND role IN('user','assistant') AND status IN('done','error')`, id, thread).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return m, ErrReaction
		}
		return m, err
	}
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return m, err
	}
	if agentID != nil {
		var member int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM thread_members WHERE thread_id=? AND agent_id=?`, thread, *agentID).Scan(&member); err != nil {
			return m, err
		}
		if member != 1 {
			return m, ErrReaction
		}
	}
	index := slices.IndexFunc(m.Reactions, func(reaction contract.Reaction) bool {
		return reaction.Emoji == request.Emoji && choiceText(reaction.AgentID) == choiceText(agentID)
	})
	if request.Remove == (index < 0) {
		return m, nil
	}
	if request.Remove {
		m.Reactions = slices.Delete(m.Reactions, index, index+1)
		if len(m.Reactions) == 0 {
			m.Reactions = nil
		}
	} else {
		m.Reactions = append(m.Reactions, contract.Reaction{Emoji: request.Emoji, AgentID: agentID})
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		return m, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET data=? WHERE id=? AND thread_id=?`, string(encoded), id, thread); err != nil {
		return m, err
	}
	if _, err := s.appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: contract.EventTypeMessageReactions, MessageID: &m.ID, Message: &m, AgentID: agentID}, nil); err != nil {
		return m, err
	}
	return m, tx.Commit()
}
