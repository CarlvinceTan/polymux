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
	if source.Status != "running" || source.Trigger != "message" {
		return run, errors.New("spawning requires a running user turn")
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
