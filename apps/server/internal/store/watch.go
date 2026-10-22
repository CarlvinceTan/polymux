package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func validateWatch(w *contract.RoutineWatch) error {
	u, err := url.Parse(w.URL)
	if err != nil || len(w.URL) > 2048 || u.Hostname() == "" || u.User != nil || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") || len(w.JsonPointers) > 8 {
		return ErrSchedule
	}
	for _, pointer := range w.JsonPointers {
		if len(pointer) > 256 || pointer != "" && !strings.HasPrefix(pointer, "/") {
			return ErrSchedule
		}
		for i := 0; i < len(pointer); i++ {
			if pointer[i] == '~' {
				if i+1 >= len(pointer) || pointer[i+1] != '0' && pointer[i+1] != '1' {
					return ErrSchedule
				}
				i++
			}
		}
	}
	return nil
}

func (s *Store) DueWatches(ctx context.Context, now time.Time) ([]contract.Routine, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM routines WHERE next_at<=?
AND json_type(data,'$.watch')='object' AND json_extract(data,'$.status')='active'
ORDER BY next_at,id LIMIT 32`, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []contract.Routine{}
	for rows.Next() {
		var raw string
		var r contract.Routine
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

// CheckWatch persists a reading only while the selected occurrence remains active.
func (s *Store) CheckWatch(ctx context.Context, selected contract.Routine, content string, checkErr error, now time.Time) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if checkErr == nil && (len(content) > 1<<20 || !utf8.ValidString(content)) {
		return ErrSchedule
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := routineTx(ctx, tx, selected.AgentID, selected.ID)
	if err != nil {
		return err
	}
	if r.Status != "active" || r.Watch == nil || valueStringWatch(r.NextAt) != valueStringWatch(selected.NextAt) {
		return nil
	}
	var previous, fingerprint string
	if err := tx.QueryRowContext(ctx, `SELECT fingerprint,content FROM routine_watches WHERE routine_id=?`, r.ID).
		Scan(&fingerprint, &previous); err != nil {
		return err
	}
	checked := now.UTC().Format(time.RFC3339)
	if checkErr == nil {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
		if fingerprint != "" && fingerprint != hash {
			detail := watchChange(previous, content)
			if _, err := tx.ExecContext(ctx, `UPDATE routine_watches SET pending=?,changed_at=? WHERE routine_id=?`,
				detail, now.Unix(), r.ID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE routine_watches SET fingerprint=?,content=?,checked_at=?,error='' WHERE routine_id=?`,
			hash, content, checked, r.ID); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE routine_watches SET checked_at=?,error=? WHERE routine_id=?`,
			checked, boundedText(checkErr.Error(), 256), r.ID); err != nil {
			return err
		}
	}
	next := now.Add(time.Duration(r.IntervalSeconds) * time.Second).UTC().Format(time.RFC3339)
	r.NextAt = &next
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE routines SET next_at=?,data=? WHERE id=?`,
		now.Unix()+r.IntervalSeconds, string(data), r.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type WatchStatus struct {
	CheckedAt string `json:"checkedAt,omitempty"`
	Error     string `json:"error,omitempty"`
	Pending   bool   `json:"pending"`
}

func (s *Store) WatchStatus(ctx context.Context, owner, id string) (WatchStatus, error) {
	var status WatchStatus
	err := s.DB.QueryRowContext(ctx, `SELECT w.checked_at,w.error,w.pending!='' FROM routine_watches w
JOIN routines r ON r.id=w.routine_id WHERE r.agent_id=? AND r.id=?`, owner, id).
		Scan(&status.CheckedAt, &status.Error, &status.Pending)
	return status, err
}

func valueStringWatch(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func watchChange(before, after string) string {
	start := 0
	for start < len(before) && start < len(after) && before[start] == after[start] {
		start++
	}
	start = max(0, start-256)
	for start > 0 && (!utf8.RuneStart(before[start]) || start < len(after) && !utf8.RuneStart(after[start])) {
		start--
	}
	return "Previously: " + boundedText(before[start:], 1800) + "\nNow: " + boundedText(after[min(start, len(after)):], 1800)
}

func watchPending(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var pending string
	if err := tx.QueryRowContext(ctx, `SELECT pending FROM routine_watches WHERE routine_id=?`, id).Scan(&pending); err != nil {
		return "", err
	}
	_, err := tx.ExecContext(ctx, `UPDATE routine_watches SET pending='',changed_at=NULL WHERE routine_id=?`, id)
	return pending, err
}
