package store

import (
	"context"
	"encoding/json"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func (s *Store) AutoMessages(ctx context.Context, thread string, before int64) ([]contract.Message, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT m.data FROM messages m
LEFT JOIN runs r ON r.id=json_extract(m.data,'$.runID')
WHERE m.thread_id=? AND m.status='done' AND (?=0 OR m.seq<?)
AND (json_extract(r.data,'$.trigger') IN('review','routine','follow_up','task','event') OR m.id LIKE 'receipt:%')
ORDER BY m.seq DESC LIMIT 16`, thread, before, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []contract.Message{}
	for rows.Next() {
		var data string
		var message contract.Message
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &message); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}
