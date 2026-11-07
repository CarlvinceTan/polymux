package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

type WorkItem struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	State      string `json:"state"`
	WaitingFor string `json:"waiting_for,omitempty"`
}

func (s *Store) OpenWork(ctx context.Context, agent, thread string) ([]WorkItem, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT data FROM open_work WHERE agent_id=? AND thread_id=? ORDER BY rowid LIMIT 64`, agent, thread,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WorkItem{}
	for rows.Next() {
		var data string
		var item WorkItem
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveWork(ctx context.Context, run contract.Run, item WorkItem) error {
	validState := item.State == "open" || item.State == "parked" || item.State == "done"
	validText := strings.TrimSpace(item.Text) != "" && len(item.Text) <= 4096
	validWait := item.WaitingFor == "" || item.State == "parked" && (item.WaitingFor == "user" || item.WaitingFor == "dependency")
	if !validState || !validText || !validWait || item.ID == "" || len(item.ID) > 128 {
		return errors.New("invalid open-work item")
	}
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM open_work
WHERE agent_id=? AND thread_id=? AND id<>?`, run.AgentID, run.ConversationID, item.ID).Scan(&count); err != nil {
		return err
	}
	if count >= 64 {
		return errors.New("open-work limit reached; remove completed items first")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO open_work(id,agent_id,thread_id,data) VALUES(?,?,?,?)
ON CONFLICT(id,agent_id,thread_id) DO UPDATE SET data=excluded.data`,
		item.ID, run.AgentID, run.ConversationID, string(data),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RemoveWork(ctx context.Context, run contract.Run, id string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM open_work WHERE agent_id=? AND thread_id=? AND id=?`, run.AgentID, run.ConversationID, id,
	)
	return err
}
