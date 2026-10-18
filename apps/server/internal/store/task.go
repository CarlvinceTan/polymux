package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"strings"
	"time"
)

var ErrTask = errors.New("task cannot continue")

type Checkpoint struct {
	Goal     string `json:"goal"`
	Next     string `json:"next"`
	Evidence string `json:"evidence"`
	After    int64  `json:"after_seconds"`
	Wait     bool   `json:"wait_for_user"`
}

type Task struct {
	ID         string `json:"id"`
	AgentID    string `json:"agentID"`
	ThreadID   string `json:"conversationID"`
	Status     string `json:"status"`
	Generation int    `json:"generation"`
	JobID      string `json:"jobID"`
	Tools      int    `json:"tools"`
	Output     int    `json:"output"`
	Checkpoint
}

func (c Checkpoint) Valid() bool {
	return strings.TrimSpace(c.Goal) != "" && len(c.Goal) <= 4096 && strings.TrimSpace(c.Next) != "" && len(c.Next) <= 8192 && len(c.Evidence) <= 8192 && c.After >= 0 && c.After <= 604800
}

func (s *Store) migrateAutomation(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS tasks(
 id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
 thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
 status TEXT NOT NULL, next_at INTEGER, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS tasks_due ON tasks(next_at);
CREATE TABLE IF NOT EXISTS effects(
 run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE, call_id TEXT NOT NULL,
 name TEXT NOT NULL, digest TEXT NOT NULL, status TEXT NOT NULL,
 PRIMARY KEY(run_id,call_id));
PRAGMA user_version=6;`)
	return err
}

func taskTx(ctx context.Context, tx *sql.Tx, id string) (Task, error) {
	var task Task
	var data, status string
	err := tx.QueryRowContext(ctx, `SELECT data,status FROM tasks WHERE id=?`, id).Scan(&data, &status)
	if err != nil {
		return task, err
	}
	err = json.Unmarshal([]byte(data), &task)
	task.Status = status
	return task, err
}

func saveTask(ctx context.Context, tx *sql.Tx, task Task, due any) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tasks(id,agent_id,thread_id,status,next_at,data) VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status,next_at=excluded.next_at,data=excluded.data`, task.ID, task.AgentID, task.ThreadID, task.Status, due, string(data))
	return err
}

func sealTask(ctx context.Context, tx *sql.Tx, run *contract.Run, checkpoint *Checkpoint, tools, output int) error {
	if checkpoint != nil {
		var peers int
		if err:=tx.QueryRowContext(ctx,`SELECT count(*) FROM deliveries WHERE json_extract(data,'$.parentRunID')=? AND json_extract(data,'$.status')='queued'`,run.ID).Scan(&peers);err!=nil{return err}
		if peers>0{return ErrTask}
	}
	var task Task
	if run.TaskID != nil {
		var err error
		task, err = taskTx(ctx, tx, *run.TaskID)
		if err != nil {
			return err
		}
		if task.Status == "cancelled" {
			return nil
		}
		if task.JobID != run.JobID || task.Status != "running" && (task.Status != "ready" || run.Status == "completed") {
			return ErrTask
		}
	} else if checkpoint != nil && run.Status == "completed" {
		task = Task{ID: "task:" + run.ID, AgentID: run.AgentID, ThreadID: run.ConversationID, JobID: run.JobID}
	} else {
		return nil
	}
	task.Tools += tools
	task.Output += output
	if run.Status != "completed" {
		task.Status = "needs_review"
		if run.Status == "cancelled" {
			task.Status = "cancelled"
		}
		return saveTask(ctx, tx, task, nil)
	}
	if checkpoint == nil {
		task.Status = "completed"
		return saveTask(ctx, tx, task, nil)
	}
	if !checkpoint.Valid() || task.Generation >= 8 || task.Tools > 128 || task.Output > 256<<10 {
		return ErrTask
	}
	var unresolved int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM effects WHERE run_id=? AND status!='returned'`, run.ID).Scan(&unresolved); err != nil {
		return err
	}
	if unresolved > 0 {
		return ErrTask
	}
	task.Checkpoint = *checkpoint
	task.Generation++
	task.Status = "sleeping"
	var due any = time.Now().Unix() + max(1, checkpoint.After)
	if checkpoint.Wait {
		task.Status = "parked"
		due = nil
	}
	run.TaskID = &task.ID
	return saveTask(ctx, tx, task, due)
}

func (s *Store) StartTask(ctx context.Context, work Work) (Task, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	task, err := taskTx(ctx, tx, work.TaskID)
	if err != nil {
		return task, err
	}
	if task.Status != "ready" || task.JobID != work.Message.ID || task.Generation != work.Generation {
		return task, ErrTask
	}
	task.Status = "running"
	if err := saveTask(ctx, tx, task, nil); err != nil {
		return task, err
	}
	return task, tx.Commit()
}

func (s *Store) Tasks(ctx context.Context, owner, thread string) ([]Task, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data,status FROM tasks WHERE agent_id=? AND thread_id=? ORDER BY rowid DESC LIMIT 64`, owner, thread)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		var task Task
		var data, status string
		if err := rows.Scan(&data, &status); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &task); err != nil {
			return nil, err
		}
		task.Status = status
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) CancelTask(ctx context.Context, owner, thread, id string) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	task, err := taskTx(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if task.AgentID != owner || task.ThreadID != thread {
		return "", ErrMembers
	}
	task.Status = "cancelled"
	if err := saveTask(ctx, tx, task, nil); err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET state='done' WHERE id=? AND state='queued'`, task.JobID)
	if err != nil {
		return "", err
	}
	run := NewRun(task.JobID, thread, owner, "task")
	if count, err := result.RowsAffected(); err != nil {
		return "", err
	} else if count > 0 {
		if err := finishRuns(ctx, tx, task.JobID); err != nil {
			return "", err
		}
	}
	return run.ID, tx.Commit()
}

func (s *Store) ResumeTask(ctx context.Context, owner, thread, id string, checkpoint Checkpoint) error {
	if !checkpoint.Valid() {
		return ErrTask
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	task, err := taskTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if task.AgentID != owner || task.ThreadID != thread {
		return ErrMembers
	}
	if task.Status != "parked" && task.Status != "needs_review" {
		return ErrTask
	}
	if task.Generation >= 8 || task.Tools >= 128 || task.Output >= 256<<10 {
		return ErrTask
	}
	task.Checkpoint = checkpoint
	task.Generation++
	task.Status = "sleeping"
	if err := saveTask(ctx, tx, task, time.Now().Unix()+1); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ScheduleTasks(ctx context.Context, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT data FROM tasks WHERE status='sleeping' AND next_at<=?
AND NOT EXISTS(SELECT 1 FROM jobs WHERE thread_id=tasks.thread_id AND state IN('queued','running'))
ORDER BY next_at,id LIMIT 32`, now.Unix())
	if err != nil {
		return err
	}
	var tasks []Task
	for rows.Next() {
		var task Task
		var data string
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(data), &task); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, task)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		var member int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM thread_members WHERE thread_id=? AND agent_id=?`, task.ThreadID, task.AgentID).Scan(&member); err != nil {
			return err
		}
		if member == 0 {
			task.Status = "cancelled"
			if err := saveTask(ctx, tx, task, nil); err != nil {
				return err
			}
			continue
		}
		var busy int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE thread_id=? AND state IN('queued','running')`, task.ThreadID).Scan(&busy); err != nil {
			return err
		}
		if busy > 0 {
			continue
		}
		id := fmt.Sprintf("task:%s:%d", task.ID, task.Generation)
		data, _ := json.Marshal(task)
		text := "Continue the established task from its sealed checkpoint, not by replaying previous calls. Tool returns are not proof of an external effect; verify its state before repeating anything. Checkpoint data:\n" + string(data)
		work := Work{Trigger: "task", TaskID: task.ID, Generation: task.Generation, Targets: []string{task.AgentID}, Routing: "explicit", Message: contract.Message{ID: id, ThreadID: task.ThreadID, Role: "system", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}}
		if err := queueTx(ctx, tx, work, task.AgentID); err != nil {
			return err
		}
		task.JobID = id
		task.Status = "ready"
		if err := saveTask(ctx, tx, task, nil); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RecoverTasks(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE tasks SET status='needs_review',next_at=NULL WHERE status='running';
UPDATE effects SET status='unknown' WHERE status='started';`)
	return err
}

func (s *Store) BeginEffect(ctx context.Context, run, call, name string, args json.RawMessage, task string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if task != "" {
		item, err := taskTx(ctx, tx, task)
		if err != nil {
			return err
		}
		if item.Status != "running" {
			return ErrTask
		}
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(args))
	_, err = tx.ExecContext(ctx, `INSERT INTO effects(run_id,call_id,name,digest,status) VALUES(?,?,?,?,'started')`, run, call, name, digest)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EndEffect(ctx context.Context, run, call string, failed bool) error {
	status := "returned"
	if failed {
		status = "unknown"
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE effects SET status=? WHERE run_id=? AND call_id=? AND status='started'`, status, run, call)
	return err
}

func (s *Store) PendingEffects(ctx context.Context, run string) (int, error) {
	var pending int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM effects WHERE run_id=? AND status!='returned'`, run).Scan(&pending)
	return pending, err
}
