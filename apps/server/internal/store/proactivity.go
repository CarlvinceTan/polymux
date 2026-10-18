package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

var ErrSchedule = errors.New("invalid schedule")

const (
	// PauseCommand and ResumeCommand are the chat commands that turn background reviews off and on.
	PauseCommand  = "pause proactivity"
	ResumeCommand = "resume proactivity"
)

const ReviewInterval = 4 * time.Hour

func (s *Store) migrateProactivity(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS routines(
 id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
 next_at INTEGER, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS routines_due ON routines(next_at);
CREATE TABLE IF NOT EXISTS proactivity(
 agent_id TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
 next_at INTEGER, data TEXT NOT NULL, last_notice TEXT NOT NULL DEFAULT '',
 recent_notices TEXT NOT NULL DEFAULT '[]');
CREATE TABLE IF NOT EXISTS open_work(
 id TEXT NOT NULL, agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
 thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE, data TEXT NOT NULL,
	 PRIMARY KEY(id,agent_id,thread_id));`)
	if err != nil {
		return err
	}
	next := time.Now().UTC().Add(ReviewInterval)
	at := next.Format(time.RFC3339)
	data, err := json.Marshal(contract.Proactivity{
		Enabled: true, IntervalSeconds: int64(ReviewInterval / time.Second), NextAt: &at,
	})
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO proactivity(agent_id,next_at,data)
SELECT t.agent_id,?,? FROM threads t WHERE t.id=t.agent_id
AND EXISTS(SELECT 1 FROM messages m WHERE m.thread_id=t.id AND m.role='user')`, next.Unix(), string(data))
	if err != nil {
		return err
	}
	return s.addNoticeWindow(ctx)
}

// addNoticeWindow is idempotent rather than version-gated, because later migrations
// rewrite user_version and a guarded migration would otherwise re-run.
func (s *Store) addNoticeWindow(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(proactivity)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var dflt any
		if err := rows.Scan(&cid, &name, &kind, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "recent_notices" {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `ALTER TABLE proactivity ADD COLUMN recent_notices TEXT NOT NULL DEFAULT '[]'`)
	return err
}

func activateReviews(ctx context.Context, tx *sql.Tx, agent, text string) error {
	next := time.Now().UTC().Add(ReviewInterval)
	at := next.Format(time.RFC3339)
	p := contract.Proactivity{Enabled: true, IntervalSeconds: int64(ReviewInterval / time.Second), NextAt: &at}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO proactivity(agent_id,next_at,data) VALUES(?,?,?)`, agent, next.Unix(), string(data),
	); err != nil {
		return err
	}
	command := strings.ToLower(strings.TrimSpace(text))
	if command == PauseCommand {
		_, err := tx.ExecContext(ctx, `UPDATE proactivity SET next_at=NULL,
data=json_set(data,'$.enabled',json('false'),'$.nextAt',NULL) WHERE agent_id=?`, agent)
		return err
	}
	if command == ResumeCommand {
		var interval int64
		if err := tx.QueryRowContext(ctx, `SELECT json_extract(data,'$.intervalSeconds') FROM proactivity
WHERE agent_id=?`, agent).Scan(&interval); err != nil {
			return err
		}
		next = time.Now().UTC().Add(time.Duration(interval) * time.Second)
		at = next.Format(time.RFC3339)
		_, err := tx.ExecContext(ctx, `UPDATE proactivity SET next_at=?,
data=json_set(data,'$.enabled',json('true'),'$.nextAt',?) WHERE agent_id=?`, next.Unix(), at, agent)
		return err
	}
	const prefix = "confirm routine "
	if strings.HasPrefix(strings.TrimSpace(text), prefix) {
		id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), prefix))
		return confirmRoutine(ctx, tx, agent, id)
	}
	return nil
}

func (s *Store) Proactivity(ctx context.Context, agent string) (contract.Proactivity, error) {
	p := contract.Proactivity{IntervalSeconds: int64(ReviewInterval / time.Second)}
	var data string
	err := s.DB.QueryRowContext(ctx, `SELECT data FROM proactivity WHERE agent_id=?`, agent).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(data), &p)
	return p, err
}

func (s *Store) SetProactivity(ctx context.Context, agent string, p contract.Proactivity) error {
	if p.IntervalSeconds < 3600 || p.IntervalSeconds > 604800 {
		return ErrSchedule
	}
	var next any
	p.NextAt = nil
	if p.Enabled {
		t := time.Now().UTC().Add(time.Duration(p.IntervalSeconds) * time.Second)
		at := t.Format(time.RFC3339)
		p.NextAt, next = &at, t.Unix()
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO proactivity(agent_id,next_at,data) VALUES(?,?,?)
ON CONFLICT(agent_id) DO UPDATE SET next_at=excluded.next_at,data=excluded.data`, agent, next, string(data))
	return err
}

func (s *Store) CreateRoutine(ctx context.Context, r contract.Routine) (contract.Routine, error) {
	at, err := time.Parse(time.RFC3339, r.At)
	validInterval := r.IntervalSeconds == 0 || r.IntervalSeconds >= 60 && r.IntervalSeconds <= 31536000
	validPrompt := strings.TrimSpace(r.Prompt) != "" && len(r.Prompt) <= 4096
	validStatus := r.Status == "pending" || r.Status == "active"
	if r.Timezone != nil {
		zone := strings.TrimSpace(*r.Timezone)
		r.Timezone = &zone
		if zone == "" {
			r.Timezone = nil
		} else if len(zone) > 64 {
			return r, ErrSchedule
		} else if _, err := time.LoadLocation(zone); err != nil {
			return r, ErrSchedule
		}
	}
	if err != nil || !validInterval || !validPrompt || !validStatus || at.Before(time.Now().Add(-time.Minute)) {
		return r, ErrSchedule
	}
	r.At = at.UTC().Format(time.RFC3339)
	r.NextAt = &r.At
	r.CreatedAt = Now()
	r.Trigger = "once"
	if r.IntervalSeconds > 0 {
		r.Trigger = "interval"
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT data FROM routines WHERE id=?`, r.ID).Scan(&previous)
	if err == nil {
		var old contract.Routine
		if err := json.Unmarshal([]byte(previous), &old); err != nil {
			return r, err
		}
		if old.AgentID != r.AgentID || old.At != r.At || old.Prompt != r.Prompt || old.IntervalSeconds != r.IntervalSeconds {
			return r, ErrConflict
		}
		return old, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM routines
WHERE agent_id=? AND json_extract(data,'$.status') IN('pending','active')`, r.AgentID).Scan(&count); err != nil {
		return r, err
	}
	if count >= 64 {
		return r, errors.New("routine limit reached")
	}
	data, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	var due any
	if r.Status == "active" {
		due = at.Unix()
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO routines(id,agent_id,next_at,data) VALUES(?,?,?,?)`, r.ID, r.AgentID, due, string(data),
	); err != nil {
		return r, err
	}
	if err := routineActivity(ctx, tx, r, "routine.created"); err != nil {
		return r, err
	}
	return r, tx.Commit()
}

func confirmRoutine(ctx context.Context, tx *sql.Tx, agent, id string) error {
	var data string
	err := tx.QueryRowContext(ctx, `SELECT data FROM routines WHERE agent_id=? AND id=?`, agent, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSchedule
	}
	if err != nil {
		return err
	}
	var r contract.Routine
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		return err
	}
	if r.Status == "active" || r.Status == "finished" {
		return nil
	}
	created, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil || r.Status != "pending" || time.Since(created) > 24*time.Hour {
		return ErrSchedule
	}
	at, err := time.Parse(time.RFC3339, r.At)
	if err != nil {
		return err
	}
	r.Status = "active"
	dataBytes, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE routines SET next_at=?,data=? WHERE id=?`, at.Unix(), string(dataBytes), id)
	return err
}

func (s *Store) ActivateRoutine(ctx context.Context, agent, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := confirmRoutine(ctx, tx, agent, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Routine(ctx context.Context, agent, id string) (contract.Routine, error) {
	var r contract.Routine
	var data string
	if err := s.DB.QueryRowContext(ctx, `SELECT data FROM routines WHERE agent_id=? AND id=?`, agent, id).Scan(&data); err != nil {
		return r, err
	}
	err := json.Unmarshal([]byte(data), &r)
	return r, err
}

func (s *Store) Routines(ctx context.Context, agent string) ([]contract.Routine, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM routines WHERE agent_id=?
ORDER BY json_extract(data,'$.status') IN('active','pending') DESC,rowid DESC LIMIT 64`, agent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	routines := []contract.Routine{}
	for rows.Next() {
		var data string
		var r contract.Routine
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			return nil, err
		}
		routines = append(routines, r)
	}
	return routines, rows.Err()
}

func (s *Store) CancelRoutine(ctx context.Context, agent, id string) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	r, err := routineTx(ctx, tx, agent, id)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE routines SET next_at=NULL,
data=json_set(data,'$.status','cancelled','$.nextAt',NULL) WHERE agent_id=? AND id=?`, agent, id)
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		if err == nil {
			err = sql.ErrNoRows
		}
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT data FROM runs WHERE agent_id=?
AND json_extract(data,'$.routineID')=? AND status IN('queued','running','waiting_input')`, agent, id)
	if err != nil {
		return nil, err
	}
	runs := []contract.Run{}
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
	for _, run := range runs {
		ids = append(ids, run.ID)
		if run.Status != "queued" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state='done' WHERE id=? AND state='queued'`, run.JobID); err != nil {
			return nil, err
		}
		run.Status, run.Summary = "cancelled", "Routine cancelled"
		if err := endRun(ctx, tx, &run); err != nil {
			return nil, err
		}
	}
	if r.Status != "cancelled" {
		if err := routineActivity(ctx, tx, r, "routine.deleted"); err != nil {
			return nil, err
		}
	}
	return ids, tx.Commit()
}

// CancelReviews withdraws an agent's queued or running reviews so pausing stops work
// rather than only silencing its notice. Reviews have no external effects to replay.
func (s *Store) CancelReviews(ctx context.Context, agent string) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT data FROM runs WHERE agent_id=?
AND json_extract(data,'$.trigger')='review' AND status IN('queued','running','waiting_input')`, agent)
	if err != nil {
		return nil, err
	}
	runs := []contract.Run{}
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
	for _, run := range runs {
		ids = append(ids, run.ID)
		if run.Status != "queued" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state='done' WHERE id=? AND state='queued'`, run.JobID); err != nil {
			return nil, err
		}
		run.Status, run.Summary = "cancelled", "Proactivity paused"
		if err := endRun(ctx, tx, &run); err != nil {
			return nil, err
		}
	}
	return ids, tx.Commit()
}

func scheduleID(kind, id string, due int64) string {
	return fmt.Sprintf("%s:%x", kind, sha256.Sum256(fmt.Appendf(nil, "%s:%d", id, due)))
}

// nextRoutineFire returns the first fire after now. A zoned routine whose interval is whole
// days keeps its local wall-clock time, so a daily 09:00 reminder does not drift across DST.
// Everything else stays on fixed elapsed intervals.
func nextRoutineFire(r contract.Routine, due, now int64) int64 {
	interval := r.IntervalSeconds
	elapsed := due + ((now-due)/interval+1)*interval
	if r.Timezone == nil || interval%(24*3600) != 0 {
		return elapsed
	}
	loc, err := time.LoadLocation(*r.Timezone)
	if err != nil {
		return elapsed
	}
	days := int(interval / (24 * 3600))
	local := time.Unix(due, 0).In(loc)
	if local.Unix() > now {
		return local.Unix()
	}
	// Jump close to now first so a long-idle routine cannot walk day by day.
	if steps := (now - due) / interval; steps > 1 {
		local = local.AddDate(0, 0, int(steps)*days)
	}
	for local.Unix() <= now {
		local = local.AddDate(0, 0, days)
	}
	return local.Unix()
}

func (s *Store) Schedule(ctx context.Context, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	type dueWork struct {
		id, agent, data, kind string
		due                   int64
	}
	rows, err := tx.QueryContext(ctx, `WITH scheduled(id,agent_id,data,due,kind) AS (
 SELECT id,agent_id,data,next_at,'routine' FROM routines WHERE next_at<=?
 UNION ALL SELECT agent_id,agent_id,data,next_at,'review' FROM proactivity WHERE next_at<=?)
SELECT id,agent_id,data,due,kind FROM scheduled
WHERE NOT EXISTS(SELECT 1 FROM runs WHERE agent_id=scheduled.agent_id
 AND status IN('queued','running','waiting_input','waiting_peer'))
AND NOT EXISTS(SELECT 1 FROM jobs WHERE thread_id=scheduled.agent_id AND state IN('queued','running'))
ORDER BY kind='routine' DESC,due,id LIMIT 32`, now.Unix(), now.Unix())
	if err != nil {
		return err
	}
	due := []dueWork{}
	for rows.Next() {
		var work dueWork
		if err := rows.Scan(&work.id, &work.agent, &work.data, &work.due, &work.kind); err != nil {
			rows.Close()
			return err
		}
		due = append(due, work)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range due {
		var busy int
		if err := tx.QueryRowContext(ctx, `SELECT
(SELECT count(*) FROM runs WHERE agent_id=? AND status IN('queued','running','waiting_input','waiting_peer'))+
(SELECT count(*) FROM jobs WHERE thread_id=? AND state IN('queued','running'))`, item.agent, item.agent).Scan(&busy); err != nil {
			return err
		}
		if busy > 0 {
			continue
		}
		text := "Review this conversation and its open work for a useful next step. Do not invent tasks, repeat old suggestions, or chase acknowledgements."
		work := Work{Trigger: item.kind, Message: contract.Message{
			ID: scheduleID(item.kind, item.id, item.due), ThreadID: item.agent,
			Role: "system", CreatedAt: now.UTC().Format(time.RFC3339Nano),
		}}
		var interval int64
		var data []byte
		var next any
		table := "routines"
		key := "id"
		if item.kind == "routine" {
			var r contract.Routine
			if err := json.Unmarshal([]byte(item.data), &r); err != nil {
				return err
			}
			if err := routineActivity(ctx, tx, r, "routine.fired"); err != nil {
				return err
			}
			text, interval, work.RoutineID = r.Prompt, r.IntervalSeconds, &r.ID
			r.NextAt, r.Status = nil, "finished"
			if interval > 0 {
				t := nextRoutineFire(r, item.due, now.Unix())
				at := time.Unix(t, 0).UTC().Format(time.RFC3339)
				r.NextAt, r.Status, next = &at, "active", t
			}
			data, err = json.Marshal(r)
		} else {
			var p contract.Proactivity
			if err := json.Unmarshal([]byte(item.data), &p); err != nil {
				return err
			}
			interval = p.IntervalSeconds
			t := now.Unix() + interval
			at := time.Unix(t, 0).UTC().Format(time.RFC3339)
			p.NextAt, next = &at, t
			data, err = json.Marshal(p)
			table, key = "proactivity", "agent_id"
		}
		if err != nil {
			return err
		}
		work.Message.Parts = []contract.Part{{Kind: "text", Text: &text}}
		if err := queueTx(ctx, tx, work, item.agent); err != nil {
			return err
		}
		// Table and key are fixed literals, never tool or user input.
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET next_at=?,data=? WHERE "+key+"=?", next, string(data), item.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) FinalizeReview(ctx context.Context, m *contract.Message, run *contract.Run) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var saved string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, run.ID).Scan(&saved); err != nil {
		return err
	}
	var previous contract.Run
	if err := json.Unmarshal([]byte(saved), &previous); err != nil {
		return err
	}
	if Terminal(previous.Status) {
		*run = previous
		return nil
	}
	text := strings.TrimSpace(Text(*m))
	if run.Status == "completed" && text != "" {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(strings.Fields(text), " "))))
		fresh, err := claimNotice(ctx, tx, run.AgentID, hash)
		if err != nil {
			return err
		}
		if fresh {
			run.MessageID, run.Outcome, run.Summary = &m.ID, textPointer("notice"), "Useful next step found"
			if _, err := appendTx(ctx, tx, contract.Event{
				ConversationID: m.ThreadID, Type: "message.start", MessageID: &m.ID, AgentID: m.AgentID, RunID: &run.ID,
			}, m); err != nil {
				return err
			}
			if _, err := appendTx(ctx, tx, contract.Event{
				ConversationID: m.ThreadID, Type: "message.done", MessageID: &m.ID, AgentID: m.AgentID, RunID: &run.ID,
			}, m); err != nil {
				return err
			}
		}
	}
	if run.MessageID == nil && run.Status == "completed" {
		run.Outcome, run.Summary = textPointer("quiet"), "Review complete; nothing new to report"
	}
	if _, err := appendTx(ctx, tx, contract.Event{
		ConversationID: run.ConversationID, Type: "run.done", AgentID: &run.AgentID, RunID: &run.ID, Run: run,
	}, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func textPointer(text string) *string { return &text }

// NoticeWindow bounds how many recent review notices are remembered.
const NoticeWindow = 8

func routineNoticeID(runID string) string { return "routine-failed:" + runID }

func routineTitle(prompt string) string {
	if title := boundedText(strings.Join(strings.Fields(prompt), " "), 80); title != "" {
		return title
	}
	return "Routine"
}

func routineReason(err *contract.Error) string {
	if err == nil || err.Code == "" || err.Code == "provider" {
		return "model error"
	}
	if err.Code == "timeout" {
		return "timed out"
	}
	if reason := boundedText(strings.Trim(strings.Join(strings.Fields(err.Message), " "), "."), 80); reason != "" {
		return reason
	}
	return "model error"
}

// routineNotice commits one failure notice before the run row is sealed.
func (s *Store) routineNotice(ctx context.Context, run *contract.Run) error {
	if run.RoutineID == nil || *run.RoutineID == "" {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status, data string
	if err := tx.QueryRowContext(ctx, `SELECT status,data FROM runs WHERE id=?`, run.ID).Scan(&status, &data); err != nil {
		return err
	}
	if Terminal(status) {
		return nil
	}
	fresh, err := claimRunNotice(ctx, tx, run.AgentID, run.ID)
	if err != nil {
		return err
	}
	if fresh {
		if err := appendRoutineNotice(ctx, tx, run); err != nil {
			return err
		}
	}
	var stored contract.Run
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		return err
	}
	if run.Error != nil {
		stored.Error = run.Error
	} else if stored.Error == nil {
		stored.Error = &contract.Error{Code: "provider", Message: "The agent could not complete this reply."}
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runs SET data=? WHERE id=? AND status=?`, string(encoded), run.ID, status); err != nil {
		return err
	}
	return tx.Commit()
}

func appendRoutineNotice(ctx context.Context, tx *sql.Tx, run *contract.Run) error {
	title := "Routine"
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT data FROM routines WHERE agent_id=? AND id=?`, run.AgentID, *run.RoutineID).Scan(&raw)
	if err == nil {
		var routine contract.Routine
		if json.Unmarshal([]byte(raw), &routine) != nil {
			return errors.New("routine data")
		}
		title = routineTitle(routine.Prompt)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	reason := routineReason(run.Error)
	text := fmt.Sprintf("Routine \u201c%s\u201d failed: %s. Retry", title, reason)
	id, agent := *run.RoutineID, run.AgentID
	args, err := json.Marshal(map[string]string{"routineID": id, "title": title, "reason": reason})
	if err != nil {
		return err
	}
	encoded := string(args)
	m := contract.Message{
		ID: routineNoticeID(run.ID), ThreadID: agent, Role: "system", Status: "done", AgentID: &agent, RunID: &run.ID, CreatedAt: Now(),
		Parts: []contract.Part{{Kind: "routine_failed", Text: &text, Name: &id, Args: &encoded}},
	}
	if _, err := appendTx(ctx, tx, contract.Event{ConversationID: agent, Type: "message.start", MessageID: &m.ID, AgentID: &agent, RunID: &run.ID}, &m); err != nil {
		return err
	}
	// Chat row stays system. Push still uses the assistant notice path.
	pushed := m
	pushed.Role = "assistant"
	return noticeTx(ctx, tx, &pushed, contract.Event{Type: "message.start"})
}

// sealRoutineFailure finishes a routine whose notice was committed but whose run
// was still open, without writing another notice.
func (s *Store) sealRoutineFailure(ctx context.Context, run *contract.Run) (bool, error) {
	if run.Trigger != "routine" {
		return false, nil
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE id=?`, routineNoticeID(run.ID)).Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	now := Now()
	if run.Error == nil {
		run.Error = &contract.Error{Code: "provider", Message: "The agent could not complete this reply."}
	}
	run.Status, run.Activity, run.Summary, run.FinishedAt = "failed", "", "Routine failed", &now
	for i := range run.Tools {
		if run.Tools[i].Status == "running" {
			run.Tools[i].Status = run.Status
			if _, err := s.Append(ctx, contract.Event{ConversationID: run.ConversationID, Type: "tool.done", RunID: &run.ID, AgentID: &run.AgentID, Tool: &run.Tools[i]}, nil); err != nil {
				return false, err
			}
		}
	}
	run.ToolID = nil
	return true, s.RunEvent(ctx, "run.done", run, nil)
}

func claimRunNotice(ctx context.Context, tx *sql.Tx, agent, runID string) (bool, error) {
	const disabled = `{"enabled":false,"intervalSeconds":14400}`
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO proactivity(agent_id,data) VALUES(?,?)`, agent, disabled); err != nil {
		return false, err
	}
	return claimWindow(ctx, tx, agent, "routine:"+runID, false)
}

// claimNotice reports whether a notice is new within the bounded recent window, so an
// older finding cannot re-notify just because a different one was reported in between.
func claimNotice(ctx context.Context, tx *sql.Tx, agent, hash string) (bool, error) {
	return claimWindow(ctx, tx, agent, hash, true)
}

func claimWindow(ctx context.Context, tx *sql.Tx, agent, hash string, enabledOnly bool) (bool, error) {
	query := `SELECT recent_notices FROM proactivity WHERE agent_id=?`
	if enabledOnly {
		query += ` AND json_extract(data,'$.enabled')=1`
	}
	var raw string
	err := tx.QueryRowContext(ctx, query, agent).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	recent := []string{}
	if err := json.Unmarshal([]byte(raw), &recent); err != nil {
		recent = []string{}
	}
	for _, seen := range recent {
		if seen == hash {
			return false, nil
		}
	}
	recent = append([]string{hash}, recent...)
	if len(recent) > NoticeWindow {
		recent = recent[:NoticeWindow]
	}
	data, err := json.Marshal(recent)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE proactivity SET recent_notices=?, last_notice=? WHERE agent_id=?`, string(data), hash, agent); err != nil {
		return false, err
	}
	return true, nil
}
