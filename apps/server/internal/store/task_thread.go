package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

const (
	taskLimit = 32
	// doneWindow is how long a finished task stays listed unless pinned.
	doneWindow = 72 * time.Hour
)

var (
	ErrTaskLimit = errors.New("too many open tasks")
	ErrTaskEdit  = errors.New("invalid task")
)

// TaskEdit changes the fields that are set.
type TaskEdit struct {
	Name        *string
	Description *string
	Status      *string
	Pinned      *bool
}

func (s *Store) migrateTaskThreads(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS task_threads(
 thread_id TEXT PRIMARY KEY REFERENCES threads(id) ON DELETE CASCADE,
 description TEXT NOT NULL, status TEXT NOT NULL, pinned_at TEXT, updated_at TEXT NOT NULL)`)
	return err
}

func validTask(name, description string) bool {
	name = strings.TrimSpace(name)
	return name != "" && len(name) <= 128 && strings.TrimSpace(description) != "" && len(description) <= 8192
}

// CreateTask adds a bot's own thread for a task and queues its first turn. A repeated id returns
// the existing task without queueing again.
func (s *Store) CreateTask(ctx context.Context, id, agent, source, name, description string) (contract.Thread, bool, error) {
	if !validTask(name, description) {
		return contract.Thread{}, false, ErrTaskEdit
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return contract.Thread{}, false, err
	}
	defer tx.Rollback()
	var exists, open int
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM threads WHERE id=?),
 (SELECT count(*) FROM task_threads k JOIN threads t ON t.id=k.thread_id WHERE t.agent_id=? AND k.status='open')`, id, agent).Scan(&exists, &open)
	if err != nil {
		return contract.Thread{}, false, err
	}
	if exists > 0 {
		if err := tx.Rollback(); err != nil {
			return contract.Thread{}, false, err
		}
		thread, err := s.Thread(ctx, id)
		return thread, false, err
	}
	if open >= taskLimit {
		return contract.Thread{}, false, ErrTaskLimit
	}
	name = strings.TrimSpace(name)
	if _, err := tx.ExecContext(ctx, `INSERT INTO threads(id,agent_id,title) VALUES(?,?,?)`, id, agent, name); err != nil {
		return contract.Thread{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO thread_members(thread_id,agent_id,position) VALUES(?,?,0)`, id, agent); err != nil {
		return contract.Thread{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_threads(thread_id,description,status,updated_at) VALUES(?,?,'open',?)`, id, description, Now()); err != nil {
		return contract.Thread{}, false, err
	}
	text := "Start this task now."
	work := Work{Trigger: contract.RunTriggerAssigned, Targets: []string{agent}, Routing: "explicit", Message: contract.Message{ID: "assign:" + id, ThreadID: id, Role: contract.MessageRoleSystem, CreatedAt: Now(), Parts: []contract.Part{{Kind: contract.PartKindText, Text: &text}}}}
	if err := s.queueTx(ctx, tx, work, agent); err != nil {
		return contract.Thread{}, false, err
	}
	activity, err := NewActivity("task.created", id, name)
	if err != nil {
		return contract.Thread{}, false, err
	}
	if err := s.activityTx(ctx, tx, source, agent, activity); err != nil {
		return contract.Thread{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return contract.Thread{}, false, err
	}
	thread, err := s.Thread(ctx, id)
	return thread, true, err
}

// UpdateTask edits a task. A non-empty owner limits it to that bot's tasks.
func (s *Store) UpdateTask(ctx context.Context, id, owner string, edit TaskEdit) (contract.Thread, error) {
	if edit.Status != nil && *edit.Status != contract.TaskInfoStatusOpen && *edit.Status != contract.TaskInfoStatusDone {
		return contract.Thread{}, ErrTaskEdit
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return contract.Thread{}, err
	}
	defer tx.Rollback()
	var agent, title, description, status string
	var pinned sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT t.agent_id,t.title,k.description,k.status,k.pinned_at FROM task_threads k JOIN threads t ON t.id=k.thread_id WHERE k.thread_id=?`, id).Scan(&agent, &title, &description, &status, &pinned)
	if err != nil {
		return contract.Thread{}, err
	}
	if owner != "" && agent != owner {
		return contract.Thread{}, sql.ErrNoRows
	}
	if edit.Name != nil {
		title = strings.TrimSpace(*edit.Name)
	}
	if edit.Description != nil {
		description = *edit.Description
	}
	if edit.Status != nil {
		status = *edit.Status
	}
	if !validTask(title, description) {
		return contract.Thread{}, ErrTaskEdit
	}
	if edit.Pinned != nil {
		switch {
		case !*edit.Pinned:
			pinned = sql.NullString{}
		case !pinned.Valid:
			pinned = sql.NullString{String: Now(), Valid: true}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET title=? WHERE id=?`, title, id); err != nil {
		return contract.Thread{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE task_threads SET description=?,status=?,pinned_at=?,updated_at=? WHERE thread_id=?`, description, status, pinned, Now(), id); err != nil {
		return contract.Thread{}, err
	}
	if err := tx.Commit(); err != nil {
		return contract.Thread{}, err
	}
	return s.Thread(ctx, id)
}

// ReopenTask puts a finished task back in progress. Other threads are unaffected.
func (s *Store) ReopenTask(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE task_threads SET status='open',updated_at=? WHERE thread_id=? AND status='done'`, Now(), id)
	return err
}

func (s *Store) IsTask(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM task_threads WHERE thread_id=?)`, id).Scan(&exists)
	return exists, err
}

// taskInfos describes tasks by thread, counting subagents still working for each.
func (s *Store) taskInfos(ctx context.Context, thread string) (map[string]*contract.TaskInfo, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT k.thread_id,k.description,k.status,k.pinned_at,k.updated_at,COALESCE(w.count,0) FROM task_threads k
LEFT JOIN (SELECT p.thread_id,count(*) count FROM runs c JOIN runs p ON p.id=json_extract(c.data,'$.parentRunID')
 WHERE c.status IN('queued','running','waiting_peer','resuming') AND json_extract(c.data,'$.trigger')='child' GROUP BY p.thread_id) w ON w.thread_id=k.thread_id
WHERE ?='' OR k.thread_id=?`, thread, thread)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*contract.TaskInfo{}
	for rows.Next() {
		var id string
		var info contract.TaskInfo
		var pinned sql.NullString
		if err := rows.Scan(&id, &info.Description, &info.Status, &pinned, &info.UpdatedAt, &info.Workers); err != nil {
			return nil, err
		}
		if pinned.Valid {
			info.PinnedAt = &pinned.String
		}
		out[id] = &info
	}
	return out, rows.Err()
}

// Chats lists what clients show: every thread except tasks that finished a while ago.
func (s *Store) Chats(ctx context.Context) ([]contract.Thread, error) {
	threads, err := s.Threads(ctx)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-doneWindow)
	return slices.DeleteFunc(threads, func(t contract.Thread) bool {
		if t.Task == nil || t.Task.Status != contract.TaskInfoStatusDone || t.Task.PinnedAt != nil {
			return false
		}
		done, err := time.Parse(time.RFC3339Nano, t.Task.UpdatedAt)
		return err == nil && done.Before(cutoff)
	}), nil
}

// AgentTasks lists a bot's tasks, unfinished first.
func (s *Store) AgentTasks(ctx context.Context, agent string) ([]contract.Thread, error) {
	threads, err := s.Chats(ctx)
	if err != nil {
		return nil, err
	}
	tasks := slices.DeleteFunc(threads, func(t contract.Thread) bool { return t.Task == nil || t.AgentID != agent })
	slices.SortStableFunc(tasks, func(a, b contract.Thread) int {
		switch {
		case a.Task.Status == b.Task.Status:
			return strings.Compare(b.Task.UpdatedAt, a.Task.UpdatedAt)
		case a.Task.Status == contract.TaskInfoStatusOpen:
			return -1
		}
		return 1
	})
	return tasks, nil
}

// TaskMessages returns the newest finished messages of a task, oldest first.
func (s *Store) TaskMessages(ctx context.Context, id string, limit int) ([]contract.Message, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM messages WHERE thread_id=? AND status='done' ORDER BY seq DESC LIMIT ?`, id, limit)
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
	slices.Reverse(messages)
	return messages, rows.Err()
}
