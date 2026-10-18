package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func endRun(ctx context.Context, tx *sql.Tx, run *contract.Run) error {
	for i := range run.Tools {
		tool := &run.Tools[i]
		if tool.Status != "running" {
			continue
		}
		tool.Status = run.Status
		if _, err := appendTx(ctx, tx, contract.Event{ConversationID: run.ConversationID, Type: "tool.done", RunID: &run.ID, AgentID: &run.AgentID, Tool: tool}, nil); err != nil {
			return err
		}
	}
	run.ToolID, run.Activity = nil, ""
	now := Now()
	run.FinishedAt = &now
	if run.MessageID != nil {
		var data string
		err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=? AND status='streaming'`, *run.MessageID).Scan(&data)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			var message contract.Message
			if err := json.Unmarshal([]byte(data), &message); err != nil {
				return err
			}
			message.Status = "error"
			if _, err := appendTx(ctx, tx, contract.Event{ConversationID: run.ConversationID, Type: "message.error", RunID: &run.ID, MessageID: run.MessageID, AgentID: &run.AgentID, Error: run.Error}, &message); err != nil {
				return err
			}
		}
	}
	_, err := appendTx(ctx, tx, contract.Event{ConversationID: run.ConversationID, Type: "run.done", RunID: &run.ID, AgentID: &run.AgentID, Run: run}, nil)
	return err
}

// endsChildren is true for failed, cancelled, and interrupted runs; a completed run keeps its children.
func endsChildren(status string) bool {
	return status == "failed" || status == "cancelled" || status == "interrupted"
}

func closeWork(ctx context.Context, tx *sql.Tx, run contract.Run) error {
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state='done' WHERE id=? AND NOT EXISTS(SELECT 1 FROM runs WHERE job_id=? AND status IN('queued','running','waiting_peer','waiting_input'))`, run.JobID, run.JobID); err != nil {
		return err
	}
	_, err := closePeers(ctx, tx, `SELECT data,input FROM deliveries WHERE json_extract(data,'$.parentRunID')=? AND json_extract(data,'$.status')='queued'`, run.ID)
	return err
}

// cancelDescendants seals non-terminal descendant runs and closes their queued deliveries and jobs.
func cancelDescendants(ctx context.Context, tx *sql.Tx, parentID string) error {
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE kids(id) AS (
SELECT id FROM runs WHERE json_extract(data,'$.parentRunID')=?
UNION
SELECT child.id FROM runs child JOIN kids ON json_extract(child.data,'$.parentRunID')=kids.id
) SELECT data FROM runs WHERE id IN(SELECT id FROM kids)`, parentID)
	if err != nil {
		return err
	}
	var runs []contract.Run
	for rows.Next() {
		var data string
		var run contract.Run
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(data), &run); err != nil {
			rows.Close()
			return err
		}
		runs = append(runs, run)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i := range runs {
		run := &runs[i]
		// A follow-up is the source continuation after peer results, including failures.
		if Terminal(run.Status) || run.Trigger == "follow_up" {
			continue
		}
		run.Status, run.Summary = "cancelled", "Parent run ended"
		run.Error = &contract.Error{Code: "stopped", Message: "The parent run ended before this work finished."}
		if err := endRun(ctx, tx, run); err != nil {
			return err
		}
	}
	if _, err := closePeers(ctx, tx, `SELECT data,input FROM deliveries WHERE json_extract(data,'$.parentRunID')=? AND json_extract(data,'$.status')='queued'`, parentID); err != nil {
		return err
	}
	for _, run := range runs {
		if run.Trigger == "follow_up" {
			continue
		}
		if err := closeWork(ctx, tx, run); err != nil {
			return err
		}
	}
	return nil
}

func descendantIDs(query func(string, ...any) (*sql.Rows, error), id string) ([]string, error) {
	rows, err := query(`WITH RECURSIVE kids(id) AS (
SELECT id FROM runs WHERE json_extract(data,'$.parentRunID')=?
UNION
SELECT child.id FROM runs child JOIN kids ON json_extract(child.data,'$.parentRunID')=kids.id
) SELECT id FROM kids`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var child string
		if err := rows.Scan(&child); err != nil {
			return nil, err
		}
		ids = append(ids, child)
	}
	return ids, rows.Err()
}

func (s *Store) DescendantIDs(ctx context.Context, id string) ([]string, error) {
	return descendantIDs(func(query string, args ...any) (*sql.Rows, error) {
		return s.DB.QueryContext(ctx, query, args...)
	}, id)
}

func (s *Store) PendingPeers(ctx context.Context, agentID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT json_extract(data,'$.runID') FROM deliveries WHERE (source_agent=? OR target_agent=?) AND json_extract(data,'$.status')='queued'`, agentID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id sql.NullString
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id.Valid {
			ids = append(ids, id.String)
		}
	}
	return ids, rows.Err()
}

func closePeers(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	type item struct {
		delivery contract.PeerDelivery
		work     Work
	}
	items := []item{}
	for rows.Next() {
		var data, input string
		var peer item
		if err := rows.Scan(&data, &input); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &peer.delivery); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(input), &peer.work); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, peer)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, peer := range items {
		run := NewRun(peer.work.Message.ID, peer.work.Message.ThreadID, peer.delivery.TargetAgentID, peer.work.Trigger)
		var data string
		err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, run.ID).Scan(&data)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			if err := json.Unmarshal([]byte(data), &run); err != nil {
				return nil, err
			}
		}
		if !Terminal(run.Status) {
			run.Status, run.Summary = "cancelled", "Peer request cancelled"
			run.Error = &contract.Error{Code: "stopped", Message: "This peer request was cancelled or a participant was removed."}
			if err := endRun(ctx, tx, &run); err != nil {
				return nil, err
			}
		}
		ids = append(ids, run.ID)
		if peer.delivery.Intent != "handoff" {
			if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state='done' WHERE id=?`, peer.work.Message.ID); err != nil {
				return nil, err
			}
		}
		if err := receiptTx(ctx, tx, peer.delivery.ID, run, contract.Message{}); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func (s *Store) StopRuns(ctx context.Context, thread string) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Deepest runs first, so an ancestor's cleanup cannot overwrite an explicit stop.
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE stopped(id, depth) AS (
SELECT id, 0 FROM runs WHERE thread_id=? AND status IN('running','waiting_peer','waiting_input')
UNION
SELECT child.id, stopped.depth+1 FROM runs child JOIN stopped ON json_extract(child.data,'$.parentRunID')=stopped.id
) SELECT r.data FROM runs r JOIN (SELECT id, max(depth) depth FROM stopped GROUP BY id) s ON r.id=s.id ORDER BY s.depth DESC`, thread)
	if err != nil {
		return nil, err
	}
	var runs []contract.Run
	for rows.Next() {
		var data string
		var run contract.Run
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &run); err != nil {
			rows.Close()
			return nil, err
		}
		runs = append(runs, run)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	ids := []string{}
	// Seal the entire tree before receipts can resolve waits or enqueue follow-ups.
	for i := range runs {
		run := &runs[i]
		if Terminal(run.Status) {
			continue
		}
		run.Status, run.Summary = "cancelled", "Reply stopped"
		run.Error = &contract.Error{Code: "stopped", Message: "Reply stopped."}
		if err := endRun(ctx, tx, run); err != nil {
			return nil, err
		}
		ids = append(ids, run.ID)
	}
	for _, run := range runs {
		if err := closeWork(ctx, tx, run); err != nil {
			return nil, err
		}
	}
	return ids, tx.Commit()
}

func (s *Store) StopWaiting(ctx context.Context, thread string) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var data string
	err = tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE thread_id=? AND status='waiting_peer' AND NOT EXISTS(SELECT 1 FROM runs active WHERE active.thread_id=? AND active.status='running') ORDER BY json_extract(data,'$.createdAt') DESC LIMIT 1`, thread, thread).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var run contract.Run
	if err := json.Unmarshal([]byte(data), &run); err != nil {
		return nil, err
	}
	run.Status, run.Summary = "cancelled", "Reply stopped"
	run.Error = &contract.Error{Code: "stopped", Message: "Reply stopped."}
	if err := endRun(ctx, tx, &run); err != nil {
		return nil, err
	}
	ids, err := closePeers(ctx, tx, `SELECT data,input FROM deliveries WHERE json_extract(data,'$.parentRunID')=? AND json_extract(data,'$.status')='queued'`, run.ID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		ids, err = descendantIDs(func(query string, args ...any) (*sql.Rows, error) {
			return tx.QueryContext(ctx, query, args...)
		}, run.ID)
		if err != nil {
			return nil, err
		}
	}
	return ids, tx.Commit()
}
