package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// Approval statuses.
const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalDenied   = "denied"
	ApprovalExpired  = "expired"
	ApprovalConsumed = "consumed"
)

// ErrApproval is returned for invalid approval decisions.
var ErrApproval = errors.New("approval decision rejected")

// Approval is a consequential tool gate record.
type Approval struct {
	ID         string          `json:"id"`
	RunID      string          `json:"runID"`
	AgentID    string          `json:"agentID"`
	ThreadID   string          `json:"conversationID"`
	Tool       string          `json:"tool"`
	Args       json.RawMessage `json:"args"`
	Key        string          `json:"idempotencyKey"`
	Status     string          `json:"status"`
	Result     json.RawMessage `json:"result,omitempty"`
	CreatedAt  string          `json:"createdAt"`
	ExpiresAt  string          `json:"expiresAt"`
	DecidedAt  *string         `json:"decidedAt,omitempty"`
	JobID      string          `json:"jobID,omitempty"`
}

// ApprovalTTL is the default pending lifetime.
const ApprovalTTL = 15 * time.Minute

func (s *Store) migrateApprovals(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS approvals(
 id TEXT PRIMARY KEY,
 run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
 agent_id TEXT NOT NULL,
 thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
 tool TEXT NOT NULL,
 args TEXT NOT NULL,
 idempotency_key TEXT NOT NULL,
 status TEXT NOT NULL,
 result TEXT,
 job_id TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 decided_at TEXT);
CREATE UNIQUE INDEX IF NOT EXISTS approvals_key ON approvals(idempotency_key);
CREATE INDEX IF NOT EXISTS approvals_run ON approvals(run_id, status);`)
	if err != nil {
		return err
	}
	return s.migrateJobResume(ctx)
}

func (s *Store) migrateJobResume(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(jobs)`)
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
		if name == "resume_requested" {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `ALTER TABLE jobs ADD COLUMN resume_requested INTEGER NOT NULL DEFAULT 0`)
	return err
}

// CreateApproval inserts a pending approval when no row exists for the key.
// If a row already exists it is returned (after lazy expiry).
func (s *Store) CreateApproval(ctx context.Context, a Approval) (Approval, bool, error) {
	if a.ID == "" || a.RunID == "" || a.Tool == "" || a.Key == "" {
		return Approval{}, false, ErrApproval
	}
	if len(a.Args) == 0 {
		a.Args = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	if a.CreatedAt == "" {
		a.CreatedAt = now.Format(time.RFC3339Nano)
	}
	if a.ExpiresAt == "" {
		a.ExpiresAt = now.Add(ApprovalTTL).Format(time.RFC3339Nano)
	}
	a.Status = ApprovalPending
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Approval{}, false, err
	}
	defer tx.Rollback()
	existing, err := approvalByKeyTx(ctx, tx, a.Key)
	if err == nil {
		existing, err = expireApprovalTx(ctx, tx, existing, now)
		if err != nil {
			return Approval{}, false, err
		}
		return existing, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Approval{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO approvals(
 id,run_id,agent_id,thread_id,tool,args,idempotency_key,status,result,job_id,created_at,expires_at,decided_at)
VALUES(?,?,?,?,?,?,?,?,NULL,?,?,?,NULL)`,
		a.ID, a.RunID, a.AgentID, a.ThreadID, a.Tool, string(a.Args), a.Key, a.Status, a.JobID, a.CreatedAt, a.ExpiresAt)
	if err != nil {
		// Unique key race: return the winner.
		existing, lookErr := approvalByKeyTx(ctx, tx, a.Key)
		if lookErr != nil {
			return Approval{}, false, err
		}
		existing, lookErr = expireApprovalTx(ctx, tx, existing, now)
		if lookErr != nil {
			return Approval{}, false, lookErr
		}
		return existing, false, tx.Commit()
	}
	return a, true, tx.Commit()
}

// Approval returns one approval by id, applying lazy expiry.
func (s *Store) Approval(ctx context.Context, id string) (Approval, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Approval{}, err
	}
	defer tx.Rollback()
	a, err := approvalByIDTx(ctx, tx, id)
	if err != nil {
		return Approval{}, err
	}
	a, err = expireApprovalTx(ctx, tx, a, time.Now().UTC())
	if err != nil {
		return Approval{}, err
	}
	return a, tx.Commit()
}

// ApprovalByKey looks up by idempotency key, applying lazy expiry.
func (s *Store) ApprovalByKey(ctx context.Context, key string) (Approval, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Approval{}, err
	}
	defer tx.Rollback()
	a, err := approvalByKeyTx(ctx, tx, key)
	if err != nil {
		return Approval{}, err
	}
	a, err = expireApprovalTx(ctx, tx, a, time.Now().UTC())
	if err != nil {
		return Approval{}, err
	}
	return a, tx.Commit()
}

// PendingApprovals lists pending (non-expired) approvals for a run.
func (s *Store) PendingApprovals(ctx context.Context, runID string) ([]Approval, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,run_id,agent_id,thread_id,tool,args,idempotency_key,status,result,job_id,created_at,expires_at,decided_at
FROM approvals WHERE run_id=? ORDER BY created_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	out := []Approval{}
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		a, err = expireApprovalTx(ctx, tx, a, now)
		if err != nil {
			return nil, err
		}
		if a.Status == ApprovalPending {
			out = append(out, a)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// DecideApproval marks pending→approved|denied. Double approve is a no-op.
// Approve after expiry returns ErrApproval with expired status persisted.
func (s *Store) DecideApproval(ctx context.Context, id, decision string) (Approval, error) {
	if decision != ApprovalApproved && decision != ApprovalDenied {
		return Approval{}, ErrApproval
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Approval{}, err
	}
	defer tx.Rollback()
	a, err := approvalByIDTx(ctx, tx, id)
	if err != nil {
		return Approval{}, err
	}
	now := time.Now().UTC()
	a, err = expireApprovalTx(ctx, tx, a, now)
	if err != nil {
		return Approval{}, err
	}
	if a.Status == ApprovalExpired {
		return a, fmt.Errorf("%w: expired", ErrApproval)
	}
	if decision == ApprovalApproved && (a.Status == ApprovalApproved || a.Status == ApprovalConsumed) {
		return a, tx.Commit()
	}
	if a.Status == ApprovalDenied {
		if decision == ApprovalDenied {
			return a, tx.Commit()
		}
		return a, fmt.Errorf("%w: denied", ErrApproval)
	}
	if a.Status != ApprovalPending {
		return a, fmt.Errorf("%w: %s", ErrApproval, a.Status)
	}
	decided := now.Format(time.RFC3339Nano)
	a.Status, a.DecidedAt = decision, &decided
	if _, err := tx.ExecContext(ctx, `UPDATE approvals SET status=?, decided_at=? WHERE id=? AND status=?`,
		a.Status, decided, a.ID, ApprovalPending); err != nil {
		return Approval{}, err
	}
	// Durable resume marker: park/finish paths requeue even if the job is not done yet.
	if a.JobID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET resume_requested=1 WHERE id=?`, a.JobID); err != nil {
			return Approval{}, err
		}
	}
	return a, tx.Commit()
}

// ClaimApproval atomically moves approved→consumed. Returns false when not claimable.
func (s *Store) ClaimApproval(ctx context.Context, key string) (Approval, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Approval{}, false, err
	}
	defer tx.Rollback()
	a, err := approvalByKeyTx(ctx, tx, key)
	if err != nil {
		return Approval{}, false, err
	}
	a, err = expireApprovalTx(ctx, tx, a, time.Now().UTC())
	if err != nil {
		return Approval{}, false, err
	}
	if a.Status != ApprovalApproved {
		return a, false, tx.Commit()
	}
	res, err := tx.ExecContext(ctx, `UPDATE approvals SET status=? WHERE id=? AND status=?`,
		ApprovalConsumed, a.ID, ApprovalApproved)
	if err != nil {
		return Approval{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Approval{}, false, err
	}
	if n != 1 {
		a, err = approvalByKeyTx(ctx, tx, key)
		return a, false, err
	}
	a.Status = ApprovalConsumed
	return a, true, tx.Commit()
}

// SaveApprovalResult stores the tool result on a consumed approval.
func (s *Store) SaveApprovalResult(ctx context.Context, id string, result json.RawMessage) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE approvals SET result=? WHERE id=? AND status=?`,
		string(result), id, ApprovalConsumed)
	return err
}

// RequeueJob sets a finished job back to queued so the actor can resume its run.
func (s *Store) RequeueJob(ctx context.Context, jobID string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE jobs SET state='queued', resume_requested=0 WHERE id=? AND state='done'`, jobID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%w: job not resumable", ErrApproval)
	}
	return nil
}

// ExpireApprovals flips overdue pending approvals to expired and requests resume.
func (s *Store) ExpireApprovals(ctx context.Context, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,run_id,agent_id,thread_id,tool,args,idempotency_key,status,result,job_id,created_at,expires_at,decided_at
FROM approvals WHERE status=?`, ApprovalPending)
	if err != nil {
		return err
	}
	var due []Approval
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			rows.Close()
			return err
		}
		due = append(due, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	jobs := map[string]bool{}
	for _, a := range due {
		expired, err := expireApprovalTx(ctx, tx, a, now)
		if err != nil {
			return err
		}
		if expired.Status == ApprovalExpired && expired.JobID != "" {
			jobs[expired.JobID] = true
		}
	}
	for jobID := range jobs {
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET resume_requested=1 WHERE id=?`, jobID); err != nil {
			return err
		}
		if err := resumeJobTx(ctx, tx, jobID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PendingApprovalCount returns pending approvals for a run (after lazy expiry).
func (s *Store) PendingApprovalCount(ctx context.Context, runID string) (int, error) {
	pending, err := s.PendingApprovals(ctx, runID)
	return len(pending), err
}

func approvalByIDTx(ctx context.Context, tx *sql.Tx, id string) (Approval, error) {
	row := tx.QueryRowContext(ctx, `SELECT id,run_id,agent_id,thread_id,tool,args,idempotency_key,status,result,job_id,created_at,expires_at,decided_at
FROM approvals WHERE id=?`, id)
	return scanApproval(row)
}

func approvalByKeyTx(ctx context.Context, tx *sql.Tx, key string) (Approval, error) {
	row := tx.QueryRowContext(ctx, `SELECT id,run_id,agent_id,thread_id,tool,args,idempotency_key,status,result,job_id,created_at,expires_at,decided_at
FROM approvals WHERE idempotency_key=?`, key)
	return scanApproval(row)
}

type approvalScanner interface {
	Scan(dest ...any) error
}

func scanApproval(row approvalScanner) (Approval, error) {
	var a Approval
	var args, result sql.NullString
	var decided sql.NullString
	err := row.Scan(&a.ID, &a.RunID, &a.AgentID, &a.ThreadID, &a.Tool, &args, &a.Key, &a.Status, &result, &a.JobID, &a.CreatedAt, &a.ExpiresAt, &decided)
	if err != nil {
		return a, err
	}
	if args.Valid {
		a.Args = json.RawMessage(args.String)
	}
	if result.Valid && result.String != "" {
		a.Result = json.RawMessage(result.String)
	}
	if decided.Valid {
		a.DecidedAt = &decided.String
	}
	return a, nil
}

func expireApprovalTx(ctx context.Context, tx *sql.Tx, a Approval, now time.Time) (Approval, error) {
	if a.Status != ApprovalPending {
		return a, nil
	}
	expires, err := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	if err != nil {
		expires, err = time.Parse(time.RFC3339, a.ExpiresAt)
		if err != nil {
			return a, err
		}
	}
	if !now.After(expires) {
		return a, nil
	}
	decided := now.Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE approvals SET status=?, decided_at=? WHERE id=? AND status=?`,
		ApprovalExpired, decided, a.ID, ApprovalPending)
	if err != nil {
		return a, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		a.Status, a.DecidedAt = ApprovalExpired, &decided
	} else {
		return approvalByIDTx(ctx, tx, a.ID)
	}
	return a, nil
}

func resolveApprovals(ctx context.Context, tx *sql.Tx, run *contract.Run) error {
	// Park while any approval is still open: pending or approved-but-unconsumed.
	var open int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM approvals WHERE run_id=? AND status IN(?,?)`,
		run.ID, ApprovalPending, ApprovalApproved).Scan(&open); err != nil {
		return err
	}
	if open == 0 {
		return nil
	}
	run.Status, run.Activity, run.Summary, run.FinishedAt = "waiting_input", "", "Waiting for approval", nil
	outcome := "approval"
	run.Outcome = &outcome
	return nil
}

// resumeJobTx requeues a done job when resume_requested is set and the run still needs input.
func resumeJobTx(ctx context.Context, tx *sql.Tx, jobID string) error {
	var flag int
	if err := tx.QueryRowContext(ctx, `SELECT resume_requested FROM jobs WHERE id=?`, jobID).Scan(&flag); err != nil {
		return err
	}
	if flag == 0 {
		return nil
	}
	var waiting int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs r WHERE r.job_id=? AND r.status='waiting_input' AND EXISTS(
SELECT 1 FROM approvals a WHERE a.run_id=r.id AND a.status IN(?,?,?))`,
		jobID, ApprovalPending, ApprovalApproved, ApprovalExpired).Scan(&waiting); err != nil {
		return err
	}
	if waiting == 0 {
		_, err := tx.ExecContext(ctx, `UPDATE jobs SET resume_requested=0 WHERE id=?`, jobID)
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE jobs SET state='queued', resume_requested=0 WHERE id=? AND state='done'`, jobID)
	return err
}

// PutPrivateRun upserts a run and its job without emitting thread events.
// Used by voice turns so the approval ledger has a real run row without leaking into chat.
func (s *Store) PutPrivateRun(ctx context.Context, run contract.Run) error {
	if run.ID == "" || run.JobID == "" || run.ConversationID == "" {
		return ErrApproval
	}
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,thread_id,request,state,input) VALUES(?,?,'{}','done','{}')
ON CONFLICT(id) DO UPDATE SET state='done'`, run.JobID, run.ConversationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO runs(id,job_id,thread_id,agent_id,status,data) VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status,data=excluded.data`,
		run.ID, run.JobID, run.ConversationID, run.AgentID, run.Status, string(data)); err != nil {
		return err
	}
	return tx.Commit()
}

// ApprovalRequestedEvent builds the run-stream event for a new pending approval.
func ApprovalRequestedEvent(a Approval) contract.Event {
	return contract.Event{
		ConversationID: a.ThreadID,
		Type:           "approval_requested",
		AgentID:        &a.AgentID,
		RunID:          &a.RunID,
		CreatedAt:      a.CreatedAt,
		Approval: &contract.ApprovalRequest{
			ID: a.ID, RunID: a.RunID, Tool: a.Tool, Args: string(a.Args), ExpiresAt: a.ExpiresAt,
		},
	}
}
