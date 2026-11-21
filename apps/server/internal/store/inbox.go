package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Delivery names for a pending input: steer joins the running turn's next
// step; queue waits for its own turn.
const (
	deliverySteer = "steer"
	deliveryQueue = "queue"
)

// migrateInbox gives each queued input an explicit admission order and delivery.
func (s *Store) migrateInbox(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(jobs)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var fallback sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &fallback, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if columns["delivery"] && columns["enqueued_seq"] {
		return nil
	}
	if !columns["delivery"] {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE jobs ADD COLUMN delivery TEXT NOT NULL DEFAULT 'queue'`); err != nil {
			return err
		}
	}
	if !columns["enqueued_seq"] {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE jobs ADD COLUMN enqueued_seq INTEGER`); err != nil {
			return err
		}
	}
	_, err = s.DB.ExecContext(ctx, `
UPDATE jobs SET delivery = CASE WHEN state='steer' THEN 'steer' ELSE 'queue' END;
UPDATE jobs SET enqueued_seq = rowid;
CREATE UNIQUE INDEX IF NOT EXISTS jobs_inbox_seq ON jobs(thread_id,enqueued_seq);`)
	return err
}

// insertJobTx admits one pending input. The sequence is per conversation and never reused.
func (s *Store) insertJobTx(ctx context.Context, tx *sql.Tx, id, thread, request string, work Work, state, delivery string) error {
	data, err := json.Marshal(work)
	if err != nil {
		return err
	}
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(enqueued_seq),0)+1 FROM jobs WHERE thread_id=?`, thread).Scan(&seq); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,thread_id,request,input,state,delivery,enqueued_seq) VALUES(?,?,?,?,?,?,?)`,
		id, thread, request, string(data), state, delivery, seq)
	return err
}
