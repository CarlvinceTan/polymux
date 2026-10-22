package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func (s *Store) Spawn(ctx context.Context, parent, call, target, task string) (contract.Run, error) {
	if parent == "" || call == "" || len(call) > 128 || target == "" || len(target) > 128 || strings.TrimSpace(task) == "" || len(task) > 8192 {
		return contract.Run{}, errors.New("invalid child task")
	}
	key, err := json.Marshal([]string{parent, call})
	if err != nil {
		return contract.Run{}, err
	}
	id := fmt.Sprintf("child:%x", sha256.Sum256(key))
	work := Work{Trigger: "child", ParentRunID: &parent, Task: task,
		Message: contract.Message{ID: id, ThreadID: target, Role: "user", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &task}}}}
	run := NewRun(id, target, target, "child")
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return run, err
	}
	defer tx.Rollback()
	var data string
	err = tx.QueryRowContext(ctx, `SELECT input FROM jobs WHERE id=?`, id).Scan(&data)
	if err == nil {
		var previous Work
		if err := json.Unmarshal([]byte(data), &previous); err != nil {
			return run, err
		}
		work.Message.CreatedAt = previous.Message.CreatedAt
		input, err := json.Marshal(work)
		if err != nil {
			return run, err
		}
		if string(input) != data {
			return run, ErrConflict
		}
		if err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, run.ID).Scan(&data); err != nil {
			return run, err
		}
		if err := json.Unmarshal([]byte(data), &run); err != nil {
			return run, err
		}
		return run, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return run, err
	}
	var source contract.Run
	if err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, parent).Scan(&data); err != nil {
		return run, err
	}
	if err := json.Unmarshal([]byte(data), &source); err != nil {
		return run, err
	}
	if source.Status != "running" || source.Trigger == "child" || source.Trigger == "created" {
		return run, errors.New("spawning requires a running bot turn")
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM thread_members WHERE thread_id=? AND agent_id=?`, target, target).Scan(&count); err != nil {
		return run, err
	}
	if count != 1 {
		return run, ErrMembers
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE thread_id=? AND state='queued'`, target).Scan(&count); err != nil {
		return run, err
	}
	if count >= 64 {
		return run, errors.New("child queue is full")
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE json_extract(data,'$.parentRunID')=? AND json_extract(data,'$.trigger')='child'`, parent).Scan(&count); err != nil {
		return run, err
	}
	if count >= 8 {
		return run, errors.New("child task limit reached")
	}
	if err := queueTx(ctx, tx, work, target); err != nil {
		return run, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, run.ID).Scan(&data); err != nil {
		return run, err
	}
	if err := json.Unmarshal([]byte(data), &run); err != nil {
		return run, err
	}
	return run, tx.Commit()
}

var ErrChild = errors.New("no child task with that ID was spawned by this bot")

const childOwner = `FROM runs c JOIN runs p ON p.id=json_extract(c.data,'$.parentRunID')
WHERE json_extract(c.data,'$.trigger')='child' AND p.agent_id=?`

// Children lists the newest child tasks spawned by an agent's runs.
func (s *Store) Children(ctx context.Context, agent string) ([]contract.Run, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.data `+childOwner+` ORDER BY json_extract(c.data,'$.createdAt') DESC LIMIT 20`, agent)
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
		out = append(out, run)
	}
	return out, rows.Err()
}

// StopChild withdraws a queued child task or stops a running one with its descendants. It
// returns the run IDs whose executions the caller must cancel.
func (s *Store) StopChild(ctx context.Context, agent, id string) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status, job string
	err = tx.QueryRowContext(ctx, `SELECT c.status,c.job_id `+childOwner+` AND c.id=?`, agent, id).Scan(&status, &job)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChild
	}
	if err != nil {
		return nil, err
	}
	ids := []string{}
	if status == "queued" {
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state='done' WHERE id=? AND state='queued'`, job); err != nil {
			return nil, err
		}
		if err := finishRuns(ctx, tx, job); err != nil {
			return nil, err
		}
	} else if !Terminal(status) {
		if ids, err = stopTree(ctx, tx, "id", id); err != nil {
			return nil, err
		}
	}
	return ids, tx.Commit()
}
