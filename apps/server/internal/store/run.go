package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

type Work struct {
	Trigger     string           `json:"trigger"`
	Message     contract.Message `json:"message"`
	ParentRunID *string          `json:"parentRunID,omitempty"`
	DeliveryID  string           `json:"deliveryID,omitempty"`
	Task        string           `json:"task,omitempty"`
	Targets     []string         `json:"targets,omitempty"`
	Routing     string           `json:"routing,omitempty"`
	RoutineID   *string          `json:"routineID,omitempty"`
	TaskID      string           `json:"taskID,omitempty"`
	Generation  int              `json:"generation,omitempty"`
}

type SendInput struct {
	ThreadID string
	Request  contract.SendRequest
	Parts    []contract.Part
	Targets  []string
	Routing  string
}

func (s *Store) migrateRuns(ctx context.Context) error {
	var version int
	if err := s.DB.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= 4 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
ALTER TABLE threads ADD COLUMN lead_agent_id TEXT NOT NULL DEFAULT '';
UPDATE threads SET lead_agent_id=agent_id;
CREATE TABLE jobs_next(id TEXT PRIMARY KEY, thread_id TEXT NOT NULL, request TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'queued', input TEXT);
INSERT INTO jobs_next(id,thread_id,request,state) SELECT id,thread_id,request,state FROM jobs ORDER BY rowid;
DROP TABLE jobs;
ALTER TABLE jobs_next RENAME TO jobs;
CREATE INDEX jobs_queue ON jobs(thread_id,state);
CREATE TABLE runs(id TEXT PRIMARY KEY, job_id TEXT NOT NULL, thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE, agent_id TEXT NOT NULL, status TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX runs_scope ON runs(thread_id,agent_id,status);
CREATE INDEX runs_job ON runs(job_id);
CREATE TABLE deliveries(id TEXT PRIMARY KEY, source_agent TEXT NOT NULL, target_agent TEXT NOT NULL, thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE, data TEXT NOT NULL, input TEXT NOT NULL);
PRAGMA user_version=4;`)
	if err != nil {
		return fmt.Errorf("migrate execution state: %w", err)
	}
	return tx.Commit()
}

func NewRun(job, thread, agent, trigger string) contract.Run {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	key, _ := json.Marshal([]string{job, thread, agent})
	id := fmt.Sprintf("run:%x", sha256.Sum256(key))
	return contract.Run{ID: id, JobID: job, ConversationID: thread, AgentID: agent, Trigger: trigger, Status: "queued", Activity: "", Attempt: 1, CreatedAt: now, UpdatedAt: now, Summary: "Queued"}
}

func queueTx(ctx context.Context, tx *sql.Tx, work Work, target string) error {
	data, err := json.Marshal(work)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,thread_id,request,input) VALUES(?,?,'{}',?)`, work.Message.ID, work.Message.ThreadID, string(data)); err != nil {
		return err
	}
	run := NewRun(work.Message.ID, work.Message.ThreadID, target, work.Trigger)
	if work.Trigger == "message" {
		run.SourceMessageID = &work.Message.ID
	}
	run.ParentRunID = work.ParentRunID
	run.RoutineID = work.RoutineID
	if work.TaskID != "" {
		run.TaskID = &work.TaskID
	}
	_, err = appendTx(ctx, tx, contract.Event{ConversationID: run.ConversationID, Type: "run.start", AgentID: &target, RunID: &run.ID, Run: &run}, nil)
	return err
}

func (s *Store) Work(ctx context.Context, id string) (Work, error) {
	var work Work
	var data sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT input FROM jobs WHERE id=?`, id).Scan(&data); err != nil {
		return work, err
	}
	if data.Valid {
		err := json.Unmarshal([]byte(data.String), &work)
		return work, err
	}
	m, err := s.Message(ctx, id)
	return Work{Trigger: "message", Message: m}, err
}

func (s *Store) Run(ctx context.Context, id string) (contract.Run, error) {
	var run contract.Run
	var data string
	if err := s.DB.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, id).Scan(&data); err != nil {
		return run, err
	}
	err := json.Unmarshal([]byte(data), &run)
	return run, err
}

func (s *Store) Runs(ctx context.Context, thread string) ([]contract.Run, error) {
	const query = `SELECT r.data FROM runs r WHERE r.thread_id=? AND
(r.status IN('queued','running','waiting_input','waiting_peer') OR NOT EXISTS(
SELECT 1 FROM runs newer WHERE newer.thread_id=r.thread_id AND newer.agent_id=r.agent_id AND
(json_extract(newer.data,'$.createdAt')>json_extract(r.data,'$.createdAt') OR
(json_extract(newer.data,'$.createdAt')=json_extract(r.data,'$.createdAt') AND
 json_extract(newer.data,'$.seq')>json_extract(r.data,'$.seq')))))
ORDER BY CASE r.status WHEN 'running' THEN 0 WHEN 'waiting_input' THEN 0 WHEN 'waiting_peer' THEN 0
WHEN 'queued' THEN 1 ELSE 2 END,json_extract(r.data,'$.createdAt') DESC`
	rows, err := s.DB.QueryContext(ctx, query, thread)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contract.Run{}
	for rows.Next() {
		var data string
		var run contract.Run
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &run); err != nil {
			return nil, err
		}
		run.Tools = []contract.ToolActivity{}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) PageRuns(ctx context.Context, thread string, before int64, limit int) ([]contract.Run, error) {
	page, err := s.Messages(ctx, thread, before, limit)
	if err != nil {
		return nil, err
	}
	out := []contract.Run{}
	seen := map[string]bool{}
	for _, message := range page {
		if message.RunID == nil || seen[*message.RunID] {
			continue
		}
		seen[*message.RunID] = true
		run, err := s.Run(ctx, *message.RunID)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func (s *Store) RunEvent(ctx context.Context, kind string, run *contract.Run, tool *contract.ToolActivity) error {
	_, err := s.Append(ctx, contract.Event{ConversationID: run.ConversationID, Type: kind, AgentID: &run.AgentID, RunID: &run.ID, Run: run, Tool: tool}, nil)
	return err
}

func (s *Store) Finalize(ctx context.Context, event contract.Event, message *contract.Message, run *contract.Run, tool *contract.ToolActivity) error {
	return s.FinalizeTask(ctx, event, message, run, tool, nil, 0, 0)
}

func (s *Store) FinalizeTask(ctx context.Context, event contract.Event, message *contract.Message, run *contract.Run, tool *contract.ToolActivity, checkpoint *Checkpoint, tools, output int) error {
	// Commit the chat notice before the run is sealed. A crash in between is
	// finished by recovery without a second notice.
	if run != nil && run.Trigger == "routine" && run.Status == "failed" {
		if err := s.routineNotice(ctx, run); err != nil {
			return err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM runs WHERE id=?`, run.ID).Scan(&status); err != nil {
		return err
	}
	if Terminal(status) {
		return nil
	}
	if err := sealTask(ctx, tx, run, checkpoint, tools, output); err != nil {
		return err
	}
	if _, err := appendTx(ctx, tx, event, message); err != nil {
		return err
	}
	if _, err := appendTx(ctx, tx, contract.Event{ConversationID: run.ConversationID, Type: "run.done", RunID: &run.ID, AgentID: &run.AgentID, Run: run, Tool: tool}, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func resolvePeers(ctx context.Context, tx *sql.Tx, run *contract.Run) error {
	var pending, failed, total int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE json_extract(data,'$.status')='queued'),count(*) FILTER(WHERE json_extract(data,'$.status') IN('failed','cancelled','interrupted')),count(*) FROM deliveries WHERE json_extract(data,'$.parentRunID')=? AND json_extract(data,'$.intent') IN('request','question')`, run.ID).Scan(&pending, &failed, &total); err != nil {
		return err
	}
	if pending > 0 {
		run.Status, run.Activity, run.Summary, run.FinishedAt = "waiting_peer", "", "Waiting for peer results", nil
	} else if failed > 0 {
		run.Status, run.Summary = "failed", "Peer work did not complete"
		run.Error = &contract.Error{Code: "peer_failed", Message: "A delegated request did not complete. Review the peer result before retrying."}
	}
	if total > 0 && pending == 0 && run.Trigger == "message" {
		return followupTx(ctx, tx, *run)
	}
	return nil
}

func Terminal(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled" || status == "interrupted"
}

func finishRuns(ctx context.Context, tx *sql.Tx, job string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status='needs_review',next_at=NULL WHERE json_extract(data,'$.jobID')=? AND status IN('running','ready')`, job); err != nil {
		return err
	}
	// waiting_input survives Finish while an approval is pending (same idea as waiting_peer).
	rows, err := tx.QueryContext(ctx, `SELECT data FROM runs WHERE status IN('queued','running') AND job_id=?`, job)
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
	for _, run := range runs {
		if run.Status == "running" && resumeReady(ctx, tx, run) {
			// The tool result is durable. Requeue so recovery can resume instead of interrupting.
			if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state='queued' WHERE id=?`, job); err != nil {
				return err
			}
			continue
		}
		if run.Status == "queued" {
			run.Status, run.Summary = "cancelled", "Not dispatched"
		} else {
			run.Status, run.Summary = "interrupted", "Outcome could not be saved"
			run.Error = &contract.Error{Code: "interrupted", Message: "Execution ended without a durable outcome. Tool effects may be incomplete; review before retrying."}
		}
		if err := endRun(ctx, tx, &run); err != nil {
			return err
		}
		var input sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT input FROM jobs WHERE id=?`, job).Scan(&input); err != nil {
			return err
		}
		var work Work
		if input.Valid {
			if err := json.Unmarshal([]byte(input.String), &work); err != nil {
				return err
			}
		}
		if work.DeliveryID != "" {
			if err := receiptTx(ctx, tx, work.DeliveryID, run, contract.Message{}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Withdraw(ctx context.Context, thread string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status='cancelled', next_at=NULL WHERE thread_id=? AND status NOT IN('completed','cancelled')`, thread); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `UPDATE jobs SET state='done' WHERE thread_id=? AND state='queued' RETURNING id`, thread)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := finishRuns(ctx, tx, id); err != nil {
			return err
		}
		if err := finishHandoffs(ctx, tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) recoverRuns(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM runs WHERE status IN('running','resuming','waiting_input','waiting_peer')`)
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
	for _, run := range runs {
		var status string
		if err := s.DB.QueryRowContext(ctx, `SELECT status FROM runs WHERE id=?`, run.ID).Scan(&status); err != nil {
			return err
		}
		if Terminal(status) {
			continue
		}
		if sealed, err := s.sealRoutineFailure(ctx, &run); err != nil {
			return err
		} else if sealed {
			continue
		}
		if run.Status == "waiting_peer" && run.MessageID != nil {
			var durable int
			if err := s.DB.QueryRowContext(ctx,
				`SELECT count(*) FROM messages WHERE id=? AND status='done'`, *run.MessageID,
			).Scan(&durable); err != nil {
				return err
			}
			if durable == 1 {
				continue
			}
		}
		if run.Status == "waiting_input" {
			var pending int
			if err := s.DB.QueryRowContext(ctx,
				`SELECT count(*) FROM approvals WHERE run_id=? AND status IN('pending','approved')`, run.ID,
			).Scan(&pending); err != nil {
				return err
			}
			if pending > 0 {
				continue
			}
		}
		if run.Status == "running" && resumeReady(ctx, s.DB, run) {
			run.Status, run.Activity, run.Summary = "resuming", "", "Resuming from the saved tool result"
			run.Attempt++
			run.Error, run.FinishedAt = nil, nil
			if err := s.RunEvent(ctx, "run.update", &run, nil); err != nil {
				return err
			}
			if _, err := s.DB.ExecContext(ctx, `UPDATE jobs SET state='queued' WHERE id=?`, run.JobID); err != nil {
				return err
			}
			continue
		}
		now := Now()
		message := "The server restarted. Tool effects may be incomplete; review before retrying."
		if run.Attempt > 1 {
			message = "The server restarted again during resume. Review before retrying."
		}
		run.Status, run.Activity, run.Summary, run.FinishedAt = "interrupted", "", "Execution interrupted", &now
		run.Error = &contract.Error{Code: "interrupted", Message: message}
		if len(run.Tools) == 0 && run.ToolID != nil {
			run.Tools = []contract.ToolActivity{{ID: *run.ToolID, RunID: run.ID, Name: "tool", Status: "running"}}
		}
		for i := range run.Tools {
			if run.Tools[i].Status == "running" {
				run.Tools[i].Status = "interrupted"
				if _, err := s.Append(ctx, contract.Event{ConversationID: run.ConversationID, Type: "tool.done", RunID: &run.ID, AgentID: &run.AgentID, Tool: &run.Tools[i]}, nil); err != nil {
					return err
				}
			}
		}
		run.ToolID = nil
		if err := s.RunEvent(ctx, "run.done", &run, nil); err != nil {
			return err
		}
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT id FROM jobs WHERE state='running'`)
	if err != nil {
		return err
	}
	var jobs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := s.Finish(ctx, job); err != nil {
			return err
		}
	}
	return s.reconcilePeers(ctx)
}

func (s *Store) Deliver(ctx context.Context, delivery contract.PeerDelivery, work Work, wake bool) (contract.PeerDelivery, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return delivery, err
	}
	defer tx.Rollback()
	input, err := json.Marshal(work)
	if err != nil {
		return delivery, err
	}
	var old, prior string
	err = tx.QueryRowContext(ctx, `SELECT data,input FROM deliveries WHERE id=?`, delivery.ID).Scan(&old, &prior)
	if err == nil {
		var previous Work
		if err := json.Unmarshal([]byte(prior), &previous); err != nil {
			return delivery, err
		}
		copy := work
		copy.Message.CreatedAt = previous.Message.CreatedAt
		comparable, err := json.Marshal(copy)
		if err != nil {
			return delivery, err
		}
		var receipt contract.PeerDelivery
		if err := json.Unmarshal([]byte(old), &receipt); err != nil {
			return delivery, err
		}
		if prior != string(comparable) || receipt.Intent != delivery.Intent || receipt.TargetAgentID != delivery.TargetAgentID || receipt.SourceAgentID != delivery.SourceAgentID || receipt.ConversationID != delivery.ConversationID {
			return delivery, ErrConflict
		}
		return receipt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return delivery, err
	}
	var parent string
	err = tx.QueryRowContext(ctx, `SELECT status FROM runs WHERE id=?`, delivery.ParentRunID).Scan(&parent)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return delivery, err
	}
	if parent == "cancelled" || parent == "interrupted" || parent == "failed" {
		return delivery, ErrConflict
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE id IN(?,?)`, delivery.SourceAgentID, delivery.TargetAgentID).Scan(&exists); err != nil {
		return delivery, err
	}
	if exists != 2 {
		return delivery, ErrMembers
	}
	kind := "peer.delivery"
	if delivery.Intent == "handoff" {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM thread_members WHERE thread_id=? AND agent_id=?`, delivery.ConversationID, delivery.TargetAgentID).Scan(&exists); err != nil {
			return delivery, err
		}
		if exists != 1 {
			return delivery, ErrMembers
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM deliveries WHERE target_agent=? AND json_extract(input,'$.message.id')=? AND json_extract(data,'$.intent')='handoff'`, delivery.TargetAgentID, work.Message.ID).Scan(&exists); err != nil {
			return delivery, err
		}
		if exists != 0 {
			return delivery, ErrConflict
		}
		run := NewRun(work.Message.ID, work.Message.ThreadID, delivery.TargetAgentID, "handoff")
		run.ParentRunID, run.SourceMessageID = work.ParentRunID, &work.Message.ID
		var previous string
		err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, run.ID).Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return delivery, err
		}
		if err == nil {
			var stored contract.Run
			if err := json.Unmarshal([]byte(previous), &stored); err != nil {
				return delivery, err
			}
			if stored.Status != "queued" {
				return delivery, ErrConflict
			}
			run.CreatedAt = stored.CreatedAt
		}
		if _, err := appendTx(ctx, tx, contract.Event{ConversationID: run.ConversationID, Type: "run.start", AgentID: &run.AgentID, RunID: &run.ID, Run: &run}, nil); err != nil {
			return delivery, err
		}
		kind = "group.handoff"
	}
	if delivery.Intent != "handoff" {
		var queued int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE thread_id=? AND state='queued'`, work.Message.ThreadID).Scan(&queued); err != nil {
			return delivery, err
		}
		if wake && queued >= 64 {
			return delivery, errors.New("peer queue is full")
		}
		incoming := work.Message
		incoming.Role, incoming.Status, incoming.AgentID = "assistant", "done", &delivery.SourceAgentID
		if _, err := appendTx(ctx, tx, contract.Event{ConversationID: incoming.ThreadID, Type: "message.start", MessageID: &incoming.ID, AgentID: incoming.AgentID}, &incoming); err != nil {
			return delivery, err
		}
	}
	if wake {
		if err := queueTx(ctx, tx, work, delivery.TargetAgentID); err != nil {
			return delivery, err
		}
	}
	if wake || delivery.Intent == "handoff" {
		id := NewRun(work.Message.ID, work.Message.ThreadID, delivery.TargetAgentID, work.Trigger).ID
		delivery.RunID = &id
	}
	data, err := json.Marshal(delivery)
	if err != nil {
		return delivery, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO deliveries(id,source_agent,target_agent,thread_id,data,input) VALUES(?,?,?,?,?,?)`, delivery.ID, delivery.SourceAgentID, delivery.TargetAgentID, delivery.ConversationID, string(data), string(input)); err != nil {
		return delivery, err
	}
	if _, err := appendTx(ctx, tx, contract.Event{ConversationID: delivery.ConversationID, Type: kind, AgentID: &delivery.SourceAgentID, RunID: &delivery.ParentRunID, Delivery: &delivery}, nil); err != nil {
		return delivery, err
	}
	if delivery.Intent == "handoff" {
		return delivery, tx.Commit()
	}
	payload, err := NewActivity("agent.messaged", delivery.ID, "")
	if err != nil {
		return delivery, err
	}
	payload.AgentIDs = []string{delivery.SourceAgentID, delivery.TargetAgentID}
	payload.MessageIDs = []string{work.Message.ID, "receipt:" + delivery.ID}
	if delivery.RunID != nil {
		payload.MessageIDs = append(payload.MessageIDs, "reply:"+*delivery.RunID)
	}
	payload.IconOrAgentID = delivery.TargetAgentID
	if err := activityTx(ctx, tx, delivery.ConversationID, delivery.SourceAgentID, payload); err != nil {
		return delivery, err
	}
	if delivery.TargetAgentID != delivery.ConversationID {
		payload.IconOrAgentID = delivery.SourceAgentID
		if err := activityTx(ctx, tx, delivery.TargetAgentID, delivery.SourceAgentID, payload); err != nil {
			return delivery, err
		}
	}
	return delivery, tx.Commit()
}

func (s *Store) Receipt(ctx context.Context, id string, run contract.Run, result contract.Message) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := receiptTx(ctx, tx, id, run, result); err != nil {
		return err
	}
	return tx.Commit()
}

func receiptTx(ctx context.Context, tx *sql.Tx, id string, run contract.Run, result contract.Message) error {
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM deliveries WHERE id=?`, id).Scan(&data); err != nil {
		return err
	}
	var delivery contract.PeerDelivery
	if err := json.Unmarshal([]byte(data), &delivery); err != nil {
		return err
	}
	if Terminal(delivery.Status) {
		return nil
	}
	var saved string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, run.ID).Scan(&saved); err != nil {
		return err
	}
	var stored contract.Run
	if err := json.Unmarshal([]byte(saved), &stored); err != nil {
		return err
	}
	if !Terminal(stored.Status) || stored.Status != run.Status || stored.AgentID != delivery.TargetAgentID || delivery.RunID == nil || *delivery.RunID != stored.ID {
		return ErrConflict
	}
	run = stored
	delivery.Status, delivery.RunID = run.Status, &run.ID
	// 1:1 timeouts stay off the thread; the parent follow-up explains them.
	timedOut := run.Error != nil && run.Error.Code == "timeout"
	if delivery.Intent != "handoff" && !timedOut {
		text := Text(result)
		if run.Status != "completed" {
			text = "Peer work " + run.Status + ". Review before retrying."
		}
		if text == "" {
			text = "Peer work completed without a text result."
		}
		text = boundedText(text, 8192)
		m := contract.Message{ID: "receipt:" + id, ThreadID: delivery.ConversationID, Role: "assistant", AgentID: &delivery.TargetAgentID, CreatedAt: Now(), Status: "done", Parts: []contract.Part{{Kind: "text", Text: &text}}}
		var parent string
		if err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, delivery.ParentRunID).Scan(&parent); err != nil {
			return err
		}
		var origin contract.Run
		if err := json.Unmarshal([]byte(parent), &origin); err != nil {
			return err
		}
		m.ReplyToID = origin.SourceMessageID
		if _, err := appendTx(ctx, tx, contract.Event{ConversationID: m.ThreadID, Type: "message.start", MessageID: &m.ID, AgentID: m.AgentID}, &m); err != nil {
			return err
		}
		payload, err := NewActivity("agent.messaged", id, "")
		if err != nil {
			return err
		}
		payload.AgentIDs = []string{delivery.SourceAgentID, delivery.TargetAgentID}
		payload.MessageIDs = []string{m.ID, "reply:" + run.ID}
		payload.IconOrAgentID = delivery.TargetAgentID
		if err := activityTx(ctx, tx, delivery.ConversationID, delivery.TargetAgentID, payload); err != nil {
			return err
		}
		if delivery.TargetAgentID != delivery.ConversationID {
			payload.IconOrAgentID = delivery.SourceAgentID
			if err := activityTx(ctx, tx, delivery.TargetAgentID, delivery.TargetAgentID, payload); err != nil {
				return err
			}
		}
	}
	encoded, err := json.Marshal(delivery)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET data=? WHERE id=?`, string(encoded), id); err != nil {
		return err
	}
	if delivery.Intent != "handoff" {
		var data string
		if err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, delivery.ParentRunID).Scan(&data); err != nil {
			return err
		}
		var parent contract.Run
		if err := json.Unmarshal([]byte(data), &parent); err != nil {
			return err
		}
		// Groups have no parent follow-up, so the source agent says the timeout in chat.
		if timedOut && parent.ConversationID != parent.AgentID {
			if err := timeoutChat(ctx, tx, delivery, parent, run); err != nil {
				return err
			}
		}
		if parent.Status == "waiting_peer" {
			parent.Status, parent.Summary = "completed", "Peer results received"
			now := Now()
			parent.FinishedAt = &now
			if err := resolvePeers(ctx, tx, &parent); err != nil {
				return err
			}
			kind := "run.done"
			if parent.Status == "waiting_peer" {
				kind = "run.update"
			}
			if _, err := appendTx(ctx, tx, contract.Event{ConversationID: parent.ConversationID, Type: kind, RunID: &parent.ID, AgentID: &parent.AgentID, Run: &parent}, nil); err != nil {
				return err
			}
		}
	}
	if _, err := appendTx(ctx, tx, contract.Event{ConversationID: delivery.ConversationID, Type: "peer.delivery", Delivery: &delivery, RunID: &delivery.ParentRunID, AgentID: &delivery.TargetAgentID}, nil); err != nil {
		return err
	}
	return nil
}

func timeoutChat(ctx context.Context, tx *sql.Tx, delivery contract.PeerDelivery, parent, child contract.Run) error {
	text := "The background task timed out and was stopped."
	if child.Error != nil && child.Error.Message != "" {
		text = child.Error.Message
	}
	text = boundedText(text, 8192)
	agent := delivery.SourceAgentID
	m := contract.Message{
		ID: "timeout:" + delivery.ID, ThreadID: delivery.ConversationID, Role: "assistant", AgentID: &agent,
		CreatedAt: Now(), Status: "done", ReplyToID: parent.SourceMessageID,
		Parts: []contract.Part{{Kind: "text", Text: &text}},
	}
	_, err := appendTx(ctx, tx, contract.Event{ConversationID: m.ThreadID, Type: "message.start", MessageID: &m.ID, AgentID: m.AgentID}, &m)
	return err
}

func finishPeer(ctx context.Context, tx *sql.Tx, job string) error {
	var input sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT input FROM jobs WHERE id=?`, job).Scan(&input); err != nil {
		return err
	}
	if !input.Valid {
		return nil
	}
	var work Work
	if err := json.Unmarshal([]byte(input.String), &work); err != nil {
		return err
	}
	if work.Trigger != "peer" || work.DeliveryID == "" {
		return nil
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM deliveries WHERE id=?`, work.DeliveryID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE job_id=?`, job).Scan(&data); err != nil {
		return err
	}
	var run contract.Run
	if err := json.Unmarshal([]byte(data), &run); err != nil {
		return err
	}
	var result contract.Message
	if run.MessageID != nil {
		err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=?`, *run.MessageID).Scan(&data)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if err := json.Unmarshal([]byte(data), &result); err != nil {
				return err
			}
		}
	}
	return receiptTx(ctx, tx, work.DeliveryID, run, result)
}

func boundedText(text string, size int) string {
	if len(text) <= size {
		return text
	}
	text = text[:size]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}

func (s *Store) reconcilePeers(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT data,input FROM deliveries WHERE json_extract(data,'$.status')='queued'`)
	if err != nil {
		return err
	}
	type pending struct {
		delivery contract.PeerDelivery
		work     Work
	}
	var peers []pending
	for rows.Next() {
		var data, input string
		var peer pending
		if err := rows.Scan(&data, &input); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(data), &peer.delivery); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(input), &peer.work); err != nil {
			rows.Close()
			return err
		}
		peers = append(peers, peer)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, peer := range peers {
		run := NewRun(peer.work.Message.ID, peer.work.Message.ThreadID, peer.delivery.TargetAgentID, peer.work.Trigger)
		stored, err := s.Run(ctx, run.ID)
		if err == nil && !Terminal(stored.Status) {
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			run = stored
		} else {
			run.Status = "interrupted"
			run.Summary = "Handoff interrupted before dispatch"
			if err := s.RunEvent(ctx, "run.done", &run, nil); err != nil {
				return err
			}
		}
		var result contract.Message
		if run.MessageID != nil {
			result, err = s.Message(ctx, *run.MessageID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if err := s.Receipt(ctx, peer.delivery.ID, run, result); err != nil {
			return err
		}
	}
	return nil
}

func finishHandoffs(ctx context.Context, tx *sql.Tx, job string) error {
	rows, err := tx.QueryContext(ctx, `SELECT data,input FROM deliveries WHERE json_extract(data,'$.intent')='handoff' AND json_extract(data,'$.status')='queued' AND json_extract(input,'$.message.id')=?`, job)
	if err != nil {
		return err
	}
	var deliveries []contract.PeerDelivery
	var works []Work
	for rows.Next() {
		var data, input string
		var delivery contract.PeerDelivery
		var work Work
		if err := rows.Scan(&data, &input); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(data), &delivery); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(input), &work); err != nil {
			rows.Close()
			return err
		}
		deliveries = append(deliveries, delivery)
		works = append(works, work)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i, delivery := range deliveries {
		work := works[i]
		run := NewRun(work.Message.ID, work.Message.ThreadID, delivery.TargetAgentID, "handoff")
		var data string
		err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, run.ID).Scan(&data)
		var stored contract.Run
		if err == nil {
			err = json.Unmarshal([]byte(data), &stored)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && Terminal(stored.Status) {
			run = stored
		} else {
			run.ParentRunID, run.Status, run.Summary = work.ParentRunID, "cancelled", "Handoff not dispatched"
			now := Now()
			run.FinishedAt = &now
			if _, err := appendTx(ctx, tx, contract.Event{ConversationID: run.ConversationID, Type: "run.done", RunID: &run.ID, AgentID: &run.AgentID, Run: &run}, nil); err != nil {
				return err
			}
		}
		if err := receiptTx(ctx, tx, delivery.ID, run, contract.Message{}); err != nil {
			return err
		}
	}
	return nil
}

type rowQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ResumeReady reports whether a running turn can continue from a saved tool result.
// A second crash (Attempt > 1) is not resumable, so recovery interrupts it.
func (s *Store) ResumeReady(ctx context.Context, run contract.Run) bool {
	return resumeReady(ctx, s.DB, run)
}

// runningTool reports an in-flight call. Finished rows stay on the run, so history alone
// must not block a resume.
func runningTool(tools []contract.ToolActivity) bool {
	return slices.ContainsFunc(tools, func(tool contract.ToolActivity) bool { return tool.Status == "running" })
}

func resumeReady(ctx context.Context, q rowQuery, run contract.Run) bool {
	if run.Attempt > 1 || run.ToolID != nil || runningTool(run.Tools) {
		return false
	}
	db, ok := openDurable(ctx, q)
	if !ok {
		return false
	}
	defer db.Close()
	var path string
	var err error
	if run.ConversationID == run.AgentID {
		err = q.QueryRowContext(ctx, `SELECT path FROM sessions WHERE agent_id=?`, run.AgentID).Scan(&path)
	} else {
		err = q.QueryRowContext(ctx, `SELECT path FROM group_sessions WHERE thread_id=? AND agent_id=?`, run.ConversationID, run.AgentID).Scan(&path)
	}
	raw, found := strings.CutPrefix(path, "durable:")
	if err != nil || !found {
		return false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return false
	}
	var kind string
	var isError int
	err = db.QueryRowContext(ctx, `SELECT json_extract(record,'$.kind'), ifnull(json_extract(record,'$.model[0].isError'), 0) FROM entries WHERE conversation_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&kind, &isError)
	if err != nil || kind != "pi.tool-result" || isError != 0 {
		return false
	}
	var live int
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE conversation_id=? AND kind='pi.tool' AND status!='terminal'`, id).Scan(&live)
	return err == nil && live == 0
}

func openDurable(ctx context.Context, q rowQuery) (*sql.DB, bool) {
	var file string
	if err := q.QueryRowContext(ctx, `SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&file); err != nil {
		return nil, false
	}
	file = strings.TrimPrefix(file, "file:")
	if cut := strings.IndexByte(file, '?'); cut >= 0 {
		file = file[:cut]
	}
	if file == "" {
		return nil, false
	}
	path := filepath.Join(filepath.Dir(file), "durable.db")
	if _, err := os.Stat(path); err != nil {
		return nil, false
	}
	db, err := sql.Open("sqlite3", "file:"+path+"?_query_only=1&_busy_timeout=5000")
	if err != nil {
		return nil, false
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, false
	}
	return db, true
}

func (s *Store) SaveSubmission(ctx context.Context, runID string, id int64) error {
	if err := s.ensureSubmissions(ctx); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO run_submissions(run_id, submission_id) VALUES(?,?) ON CONFLICT(run_id) DO UPDATE SET submission_id=excluded.submission_id`, runID, id)
	return err
}

func (s *Store) Submission(ctx context.Context, runID string) (int64, bool) {
	if err := s.ensureSubmissions(ctx); err != nil {
		return 0, false
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT submission_id FROM run_submissions WHERE run_id=?`, runID).Scan(&id)
	return id, err == nil
}

func (s *Store) ensureSubmissions(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS run_submissions(run_id TEXT PRIMARY KEY, submission_id INTEGER NOT NULL)`)
	return err
}
