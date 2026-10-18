package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func (s *Store) migrateNotices(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS notices(
 seq INTEGER PRIMARY KEY AUTOINCREMENT, thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
 message_id TEXT REFERENCES messages(id) ON DELETE CASCADE, notice_key TEXT NOT NULL UNIQUE, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS push_devices(id TEXT PRIMARY KEY,token TEXT NOT NULL,environment TEXT NOT NULL,server TEXT NOT NULL,enabled INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS push_queue(
 notice_seq INTEGER NOT NULL REFERENCES notices(seq) ON DELETE CASCADE,
 device_id TEXT NOT NULL REFERENCES push_devices(id) ON DELETE CASCADE,
 state TEXT NOT NULL DEFAULT 'pending',attempts INTEGER NOT NULL DEFAULT 0,retry_at INTEGER,PRIMARY KEY(notice_seq,device_id));`)
	if err != nil {
		return err
	}
	if err := s.addPushRetry(ctx); err != nil {
		return err
	}
	var schema string
	if err := s.DB.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name='notices'`).Scan(&schema); err != nil {
		return err
	}
	if !strings.Contains(schema, "message_id TEXT NOT NULL UNIQUE") {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE saved_push AS SELECT * FROM push_queue;
CREATE TABLE notices_next(seq INTEGER PRIMARY KEY AUTOINCREMENT, thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE, message_id TEXT REFERENCES messages(id) ON DELETE CASCADE, notice_key TEXT NOT NULL UNIQUE, data TEXT NOT NULL);
INSERT INTO notices_next SELECT seq,thread_id,message_id,message_id,json_set(data,'$.body','New message in Polymux','$.outcome','done') FROM notices;
DROP TABLE push_queue;
DROP TABLE notices;
ALTER TABLE notices_next RENAME TO notices;
CREATE TABLE push_queue(notice_seq INTEGER NOT NULL REFERENCES notices(seq) ON DELETE CASCADE, device_id TEXT NOT NULL REFERENCES push_devices(id) ON DELETE CASCADE, state TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0, retry_at INTEGER, PRIMARY KEY(notice_seq,device_id));
INSERT INTO push_queue SELECT * FROM saved_push;
DROP TABLE saved_push;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// addPushRetry is idempotent rather than version-gated, because later migrations rewrite user_version.
func (s *Store) addPushRetry(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(push_queue)`)
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
		if name == "attempts" {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `ALTER TABLE push_queue ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0`)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `ALTER TABLE push_queue ADD COLUMN retry_at INTEGER`)
	return err
}

func noticeTx(ctx context.Context, tx *sql.Tx, m *contract.Message, event contract.Event) error {
	if event.Run != nil && event.Run.Trigger == "message" {
		return turnNotice(ctx, tx, event.Run)
	}
	if m == nil || m.Role != "assistant" || m.Status != "done" || event.Type != "message.done" && event.Type != "message.start" {
		return nil
	}
	content := Text(*m) != ""
	for _, part := range m.Parts {
		content = content || part.Attachment != nil && part.Attachment.Kind == "html"
	}
	if !content {
		return nil
	}
	var trigger string
	if m.RunID != nil {
		err := tx.QueryRowContext(ctx, `SELECT json_extract(data,'$.trigger') FROM runs WHERE id=?`, *m.RunID).Scan(&trigger)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if trigger == "created" || trigger == "peer" || trigger == "message" {
			return nil
		}
	}
	title := "Polymux"
	if m.AgentID != nil {
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT json_extract(data,'$.name') FROM agents WHERE id=?`, *m.AgentID).Scan(&name); err == nil && name != "" {
			title = name
		}
	}
	notice := contract.Notice{ConversationID: m.ThreadID, MessageID: m.ID, Title: title, Body: "New message in Polymux", Outcome: "done", CreatedAt: Now()}
	return saveNotice(ctx, tx, notice, m.ID, m.ID)
}

func turnNotice(ctx context.Context, tx *sql.Tx, run *contract.Run) error {
	outcome, body := "", ""
	switch run.Status {
	case "completed":
		outcome, body = "done", "Finished"
	case "failed":
		outcome, body = "failed", "Failed"
		if run.Error != nil {
			body += ": " + run.Error.Message
		}
	case "waiting_input":
		outcome, body = "waiting_input", "Needs your approval"
	default:
		return nil
	}
	var data string
	var messageID any
	err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE json_extract(data,'$.runID')=? AND json_extract(data,'$.role')='assistant' AND json_extract(data,'$.status')='done' ORDER BY rowid DESC LIMIT 1`, run.ID).Scan(&data)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		var m contract.Message
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return err
		}
		messageID = m.ID
		if outcome == "done" && strings.TrimSpace(Text(m)) != "" {
			body = Text(m)
		}
	}
	title := "Polymux"
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT json_extract(data,'$.name') FROM agents WHERE id=?`, run.AgentID).Scan(&name); err == nil && name != "" {
		title = name
	}
	id, _ := messageID.(string)
	return saveNotice(ctx, tx, contract.Notice{ConversationID: run.ConversationID, MessageID: id, Title: title, Body: noticeBody(body), Outcome: outcome, CreatedAt: Now()}, messageID, run.ID+":"+outcome)
}

func noticeBody(body string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	chars := []rune(strings.TrimSpace(line))
	if len(chars) > 140 {
		chars = chars[:140]
	}
	return string(chars)
}

func saveNotice(ctx context.Context, tx *sql.Tx, notice contract.Notice, messageID any, key string) error {
	data, _ := json.Marshal(notice)
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO notices(thread_id,message_id,notice_key,data)VALUES(?,?,?,?)`, notice.ConversationID, messageID, key, string(data))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return err
	}
	notice.Seq = seq
	data, _ = json.Marshal(notice)
	if _, err := tx.ExecContext(ctx, `UPDATE notices SET data=? WHERE seq=?`, string(data), seq); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO push_queue(notice_seq,device_id) SELECT ?,id FROM push_devices WHERE enabled=1`, seq)
	return err
}

func (s *Store) Notices(ctx context.Context, after *int64) (contract.NoticePage, error) {
	page := contract.NoticePage{Notices: []contract.Notice{}}
	if after == nil {
		err := s.DB.QueryRowContext(ctx, `SELECT coalesce(max(seq),0) FROM notices`).Scan(&page.Cursor)
		return page, err
	}
	page.Cursor = *after
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM notices WHERE seq>? ORDER BY seq LIMIT 100`, *after)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		var notice contract.Notice
		if err := rows.Scan(&data); err != nil {
			return page, err
		}
		if err := json.Unmarshal([]byte(data), &notice); err != nil {
			return page, err
		}
		page.Notices = append(page.Notices, notice)
		page.Cursor = notice.Seq
	}
	return page, rows.Err()
}

var ErrDevice = errors.New("invalid push device")

func (s *Store) RegisterPush(ctx context.Context, device contract.PushDevice) error {
	if len(device.ID) < 16 || len(device.ID) > 128 || len(device.Server) > 2048 || device.Environment != "sandbox" && device.Environment != "production" {
		return ErrDevice
	}
	if device.Enabled || device.Token != "" {
		token, err := hex.DecodeString(device.Token)
		if err != nil || len(token) < 16 || len(token) > 128 {
			return ErrDevice
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM push_devices WHERE id!=?`, device.ID).Scan(&count); err != nil {
		return err
	}
	if count >= 64 {
		return ErrDevice
	}
	if _, err := tx.ExecContext(ctx, `UPDATE push_queue SET state='cancelled' WHERE device_id=? AND state='pending'
AND (?=0 OR EXISTS(SELECT 1 FROM push_devices WHERE id=? AND (token!=? OR environment!=? OR server!=?)))`, device.ID, device.Enabled, device.ID, device.Token, device.Environment, device.Server); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO push_devices(id,token,environment,server,enabled) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET token=excluded.token,environment=excluded.environment,server=excluded.server,enabled=excluded.enabled`, device.ID, device.Token, device.Environment, device.Server, device.Enabled)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type Push struct {
	Notice contract.Notice
	Device contract.PushDevice
}

// PushResult is the outcome of one provider attempt.
// HasAfter means After is a Retry-After delay, already capped by the caller deadline.
type PushResult struct {
	State    string
	After    time.Duration
	HasAfter bool
}

func (s *Store) ClaimPush(ctx context.Context) (Push, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Push{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE push_queue SET state='expired' WHERE state='pending' AND notice_seq IN(
SELECT seq FROM notices WHERE julianday(json_extract(data,'$.createdAt'))<julianday('now','-1 hour'))`); err != nil {
		return Push{}, err
	}
	var push Push
	var data string
	err = tx.QueryRowContext(ctx, `SELECT n.data,d.id,d.token,d.environment,d.server FROM push_queue q JOIN notices n ON n.seq=q.notice_seq JOIN push_devices d ON d.id=q.device_id
WHERE q.state='pending' AND d.enabled=1 AND (q.retry_at IS NULL OR q.retry_at<=?)
ORDER BY q.notice_seq,d.id LIMIT 1`, time.Now().Unix()).Scan(&data, &push.Device.ID, &push.Device.Token, &push.Device.Environment, &push.Device.Server)
	if err != nil {
		return push, err
	}
	if err := json.Unmarshal([]byte(data), &push.Notice); err != nil {
		return push, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE push_queue SET state='sending',attempts=attempts+1 WHERE notice_seq=? AND device_id=?`, push.Notice.Seq, push.Device.ID); err != nil {
		return push, err
	}
	return push, tx.Commit()
}

// PushAttempts is the first try plus one retry. An ambiguous send is never replayed.
const PushAttempts = 2

func (s *Store) FinishPush(ctx context.Context, push Push, result PushResult) error {
	state := result.State
	if state != "sent" && state != "failed" && state != "unknown" && state != "invalid" && state != "rejected" {
		return ErrDevice
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Only a definite, retryable failure is queued again. 400 and 403 are rejected.
	if state == "failed" {
		var attempts int
		if err := tx.QueryRowContext(ctx, `SELECT attempts FROM push_queue WHERE notice_seq=? AND device_id=?`, push.Notice.Seq, push.Device.ID).Scan(&attempts); err != nil {
			return err
		}
		if attempts < PushAttempts {
			backoff := time.Duration(1<<attempts) * time.Minute
			if result.HasAfter {
				backoff = result.After
				if backoff < 0 {
					backoff = 0
				}
			}
			if _, err := tx.ExecContext(ctx, `UPDATE push_queue SET state='pending',retry_at=? WHERE notice_seq=? AND device_id=? AND state='sending'`,
				time.Now().Add(backoff).Unix(), push.Notice.Seq, push.Device.ID); err != nil {
				return err
			}
			return tx.Commit()
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE push_queue SET state=? WHERE notice_seq=? AND device_id=? AND state='sending'`, state, push.Notice.Seq, push.Device.ID); err != nil {
		return err
	}
	if state == "invalid" {
		if _, err := tx.ExecContext(ctx, `UPDATE push_devices SET enabled=0 WHERE id=? AND token=?`, push.Device.ID, push.Device.Token); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PruneNotices drops settled delivery rows and old feed entries. The feed pages by
// sequence, so removing old rows cannot open a gap for a client holding an older cursor.
func (s *Store) PruneNotices(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM push_queue WHERE state IN('sent','expired','invalid')
AND notice_seq IN(SELECT seq FROM notices WHERE julianday(json_extract(data,'$.createdAt'))<julianday('now','-7 days'))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM notices WHERE julianday(json_extract(data,'$.createdAt'))<julianday('now','-30 days')`); err != nil {
		return err
	}
	return tx.Commit()
}
