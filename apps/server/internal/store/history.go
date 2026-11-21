package store

import (
	"context"
	"encoding/json"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func (s *Store) Member(ctx context.Context, agent, thread string) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM thread_members WHERE thread_id=? AND agent_id=?`, thread, agent).Scan(&count)
	return count > 0, err
}

type HistoryMessage struct {
	contract.Message
	Snippet string
}

const historyScope = `m.thread_id=? AND m.seq<?
AND m.status='done' AND m.role IN('user','assistant')
AND EXISTS(SELECT 1 FROM thread_members tm WHERE tm.thread_id=m.thread_id AND tm.agent_id=?)`

const historySnippet = `snippet(messages_fts,2,char(1),char(2),'…',64)`
const historyMatches = ` FROM messages m JOIN messages_fts ON m.id=messages_fts.message_id
WHERE ` + historyScope + ` AND messages_fts MATCH ? AND messages_fts.thread_id=?
ORDER BY bm25(messages_fts), m.seq DESC LIMIT ?`

func historyBounds(before int64, limit int) (int64, int) {
	if before <= 0 {
		before = 1 << 62
	}
	if limit <= 0 || limit > 24 {
		limit = 24
	}
	return before, limit
}

func (s *Store) History(ctx context.Context, agent, thread, query string, before int64, limit int) ([]HistoryMessage, error) {
	before, limit = historyBounds(before, limit)
	if query != "" {
		return s.historyRows(ctx, `SELECT m.data,`+historySnippet+historyMatches, thread, before, agent, query, thread, limit)
	}
	return s.historyRows(ctx, `SELECT m.data,'' FROM messages m WHERE `+historyScope+` ORDER BY m.seq DESC LIMIT ?`, thread, before, agent, limit)
}

func (s *Store) historyRows(ctx context.Context, statement string, args ...any) ([]HistoryMessage, error) {
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []HistoryMessage{}
	for rows.Next() {
		var data string
		var item HistoryMessage
		if err := rows.Scan(&data, &item.Snippet); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &item.Message); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
