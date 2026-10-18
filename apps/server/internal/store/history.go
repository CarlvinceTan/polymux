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

func (s *Store) History(ctx context.Context, agent, thread, query string, before int64, limit int) ([]contract.Message, error) {
	if before <= 0 {
		before = 1 << 62
	}
	if limit <= 0 || limit > 24 {
		limit = 24
	}
	statement := `SELECT m.data FROM messages m WHERE m.thread_id=? AND m.seq<?
AND m.status='done' AND m.role IN('user','assistant')
AND EXISTS(SELECT 1 FROM thread_members tm WHERE tm.thread_id=m.thread_id AND tm.agent_id=?)`
	args := []any{thread, before, agent}
	if query != "" {
		statement += ` AND m.id IN(SELECT message_id FROM messages_fts WHERE messages_fts MATCH ? AND thread_id=?)`
		args = append(args, query, thread)
	}
	statement += ` ORDER BY m.seq DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []contract.Message{}
	for rows.Next() {
		var data string
		var m contract.Message
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}
