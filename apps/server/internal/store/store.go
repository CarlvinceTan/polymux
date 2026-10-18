package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct{ DB *sql.DB }

func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	vec.Auto()
	db, err := sql.Open("sqlite3", "file:"+url.PathEscape(path)+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{DB: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize store (build with -tags sqlite_fts5): %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.DB.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS agents(id TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS agent_definitions(agent_id TEXT PRIMARY KEY REFERENCES agents(id), data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS deleted_agents(id TEXT PRIMARY KEY, deleted_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS threads(id TEXT PRIMARY KEY, agent_id TEXT UNIQUE NOT NULL REFERENCES agents(id), last_read_seq INTEGER NOT NULL DEFAULT 0, last_seq INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS events(thread_id TEXT NOT NULL REFERENCES threads(id), seq INTEGER NOT NULL, data TEXT NOT NULL, PRIMARY KEY(thread_id,seq));
CREATE TABLE IF NOT EXISTS messages(id TEXT PRIMARY KEY, thread_id TEXT NOT NULL REFERENCES threads(id), seq INTEGER NOT NULL, role TEXT NOT NULL, status TEXT NOT NULL, data TEXT NOT NULL, indexing_state TEXT NOT NULL DEFAULT 'pending');
CREATE INDEX IF NOT EXISTS messages_history ON messages(thread_id,seq);
CREATE TABLE IF NOT EXISTS attachments(id TEXT PRIMARY KEY, data TEXT NOT NULL, path TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY REFERENCES messages(id), thread_id TEXT NOT NULL, request TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'queued');
CREATE TABLE IF NOT EXISTS sessions(agent_id TEXT PRIMARY KEY, path TEXT NOT NULL);
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(message_id UNINDEXED, thread_id UNINDEXED, body);
CREATE TABLE IF NOT EXISTS media_index(message_id TEXT PRIMARY KEY REFERENCES messages(id), text TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS media_vectors(message_id TEXT PRIMARY KEY REFERENCES messages(id), thread_id TEXT NOT NULL, embedding BLOB NOT NULL, model TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS vectors_scope ON media_vectors(thread_id);`)
	if err != nil {
		return err
	}
	if version < 2 {
		_, err = s.DB.ExecContext(ctx, `UPDATE messages SET seq=COALESCE((SELECT min(e.seq) FROM events e WHERE e.thread_id=messages.thread_id AND json_extract(e.data,'$.messageID')=messages.id),seq);
UPDATE messages SET data=json_set(data,'$.seq',seq);
UPDATE events SET data=json_set(data,'$.message.seq',(SELECT m.seq FROM messages m WHERE m.id=json_extract(events.data,'$.message.id'))) WHERE json_type(data,'$.message')='object';
PRAGMA user_version=2;`)
	}
	if err != nil {
		return err
	}
	if err := s.migrateGroups(ctx); err != nil {
		return err
	}
	if err := s.migrateRuns(ctx); err != nil {
		return err
	}
	if err := s.migrateProactivity(ctx); err != nil {
		return err
	}
	if err := s.migrateAutomation(ctx); err != nil {
		return err
	}
	if err := s.migrateTriggers(ctx); err != nil {
		return err
	}
	if err := s.migrateNotices(ctx); err != nil {
		return err
	}
	if err := s.migrateApprovals(ctx); err != nil {
		return err
	}
	if err := s.migratePins(ctx); err != nil {
		return err
	}
	if err := s.migrateSections(ctx); err != nil {
		return err
	}
	if err := s.migrateMCP(ctx); err != nil {
		return err
	}
	return s.migrateSidebar(ctx)
}

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (s *Store) SaveDefinition(ctx context.Context, a contract.Agent, definition json.RawMessage) error {
	return s.saveDefinition(ctx, a, definition, false, nil, nil)
}

func (s *Store) CreateDefinition(ctx context.Context, a contract.Agent, definition json.RawMessage, task, section *string) error {
	return s.saveDefinition(ctx, a, definition, true, task, section)
}

func (s *Store) saveDefinition(ctx context.Context, a contract.Agent, definition json.RawMessage, intro bool, task, section *string) error {
	if !json.Valid(definition) {
		return fmt.Errorf("invalid agent definition")
	}
	a.Active = false
	if a.Avatar == nil {
		if avatar := contract.GrokBotAvatarFromDefinition(definition); avatar != nil {
			a.Avatar = avatar
		}
	}
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before contract.SidebarSnapshot
	if intro {
		before, err = sidebarSnapshot(ctx, tx)
		if err != nil {
			return err
		}
	}
	if section != nil {
		if _, err := sectionScan(tx.QueryRowContext(ctx, sectionSelect+` WHERE s.id=?`, *section)); errors.Is(err, sql.ErrNoRows) {
			return ErrSectionMissing
		} else if err != nil {
			return err
		}
	}
	var deleted int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM deleted_agents WHERE id=?`, a.ID).Scan(&deleted); err != nil {
		return err
	}
	if deleted != 0 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agents(id,data) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, a.ID, string(data)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO threads(id,agent_id) VALUES(?,?)`, a.ID, a.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO thread_members(thread_id,agent_id,position) VALUES(?,?,0)`, a.ID, a.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_definitions(agent_id,data) VALUES(?,?) ON CONFLICT(agent_id) DO UPDATE SET data=excluded.data`, a.ID, string(definition)); err != nil {
		return err
	}
	if intro {
		if _, err := tx.ExecContext(ctx, `UPDATE threads SET section_id=?,section_order=(SELECT COALESCE(max(section_order)+1,0) FROM threads WHERE section_id IS ?) WHERE id=?`, section, section, a.ID); err != nil {
			return err
		}
		text := "You were just created. In one reply, say what you understood your role to be from your role and instructions, and ask for anything you need to get started. Do not perform work or use tools."
		work := Work{Trigger: "created", Message: contract.Message{ID: "created:" + a.ID, ThreadID: a.ID, Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}}, CreatedAt: Now()}}
		if err := queueTx(ctx, tx, work, a.ID); err != nil {
			return err
		}
		if task != nil && strings.TrimSpace(*task) != "" {
			m := contract.Message{ID: "initial:" + a.ID, ThreadID: a.ID, Role: "user", Parts: []contract.Part{{Kind: "text", Text: task}}, Status: "done", CreatedAt: Now()}
			if _, err := appendTx(ctx, tx, contract.Event{ConversationID: a.ID, Type: "message.start", MessageID: &m.ID}, &m); err != nil {
				return err
			}
			if err := queueTx(ctx, tx, Work{Trigger: "message", Message: m}, a.ID); err != nil {
				return err
			}
			if err := activateReviews(ctx, tx, a.ID, ""); err != nil {
				return err
			}
		}
	}
	if intro {
		return sidebarCommit(ctx, tx, before, "Created "+a.Name, nil, nil)
	}
	return tx.Commit()
}

func (s *Store) Deleted(ctx context.Context, id string) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM deleted_agents WHERE id=?`, id).Scan(&count)
	return count != 0, err
}

func (s *Store) DeleteAgent(ctx context.Context, id string) ([]string, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	before, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE id=?`, id).Scan(&exists); err != nil {
		return nil, false, err
	}
	if _, err := closePeers(ctx, tx, `SELECT data,input FROM deliveries WHERE (source_agent=? OR target_agent=?) AND json_extract(data,'$.status')='queued'`, id, id); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM group_sessions WHERE agent_id=?`, id); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_members WHERE agent_id=?`, id); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET agent_id=(SELECT agent_id FROM thread_members WHERE thread_id=threads.id ORDER BY position LIMIT 1) WHERE id<>agent_id AND agent_id=? AND EXISTS(SELECT 1 FROM thread_members WHERE thread_id=threads.id)`, id); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET lead_agent_id=agent_id WHERE lead_agent_id=?`, id); err != nil {
		return nil, false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.path FROM attachments a
WHERE EXISTS(SELECT 1 FROM messages m, json_each(m.data,'$.parts') p WHERE m.thread_id IN(SELECT t.id FROM threads t WHERE t.id=? OR NOT EXISTS(SELECT 1 FROM thread_members WHERE thread_id=t.id)) AND json_extract(p.value,'$.attachment.id')=a.id)
AND NOT EXISTS(SELECT 1 FROM messages m, json_each(m.data,'$.parts') p WHERE m.thread_id NOT IN(SELECT t.id FROM threads t WHERE t.id=? OR NOT EXISTS(SELECT 1 FROM thread_members WHERE thread_id=t.id)) AND json_extract(p.value,'$.attachment.id')=a.id)`, id, id)
	if err != nil {
		return nil, false, err
	}
	var files, attachments []string
	for rows.Next() {
		var attachment, path string
		if err := rows.Scan(&attachment, &path); err != nil {
			rows.Close()
			return nil, false, err
		}
		attachments = append(attachments, attachment)
		files = append(files, path)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deleted_agents(id,deleted_at) SELECT id,? FROM agents WHERE id=?`, Now(), id); err != nil {
		return nil, false, err
	}
	queries := []string{
		`DELETE FROM runs WHERE thread_id IN(SELECT t.id FROM threads t WHERE t.id=? OR NOT EXISTS(SELECT 1 FROM thread_members WHERE thread_id=t.id))`,
		`DELETE FROM jobs WHERE thread_id IN(SELECT t.id FROM threads t WHERE t.id=? OR NOT EXISTS(SELECT 1 FROM thread_members WHERE thread_id=t.id))`,
		`DELETE FROM media_vectors WHERE thread_id IN(SELECT t.id FROM threads t WHERE t.id=? OR NOT EXISTS(SELECT 1 FROM thread_members WHERE thread_id=t.id))`,
		`DELETE FROM media_index WHERE message_id IN(SELECT m.id FROM messages m JOIN threads t ON t.id=m.thread_id WHERE t.id=? OR NOT EXISTS(SELECT 1 FROM thread_members WHERE thread_id=t.id))`,
		`DELETE FROM messages_fts WHERE thread_id IN(SELECT t.id FROM threads t WHERE t.id=? OR NOT EXISTS(SELECT 1 FROM thread_members WHERE thread_id=t.id))`,
		`DELETE FROM events WHERE thread_id IN(SELECT t.id FROM threads t WHERE t.id=? OR NOT EXISTS(SELECT 1 FROM thread_members WHERE thread_id=t.id))`,
		`DELETE FROM messages WHERE thread_id IN(SELECT t.id FROM threads t WHERE t.id=? OR NOT EXISTS(SELECT 1 FROM thread_members WHERE thread_id=t.id))`,
		`DELETE FROM threads WHERE id=? OR NOT EXISTS(SELECT 1 FROM thread_members WHERE thread_id=threads.id)`,
		`DELETE FROM sessions WHERE agent_id=?`,
		`DELETE FROM agent_definitions WHERE agent_id=?`,
		`DELETE FROM agents WHERE id=?`,
	}
	for _, query := range queries {
		args := []any{id}
		if strings.Contains(query, "source_agent") {
			args = append(args, id)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return nil, false, err
		}
	}
	for _, attachment := range attachments {
		if _, err := tx.ExecContext(ctx, `DELETE FROM attachments WHERE id=?`, attachment); err != nil {
			return nil, false, err
		}
	}
	return files, exists != 0, sidebarCommit(ctx, tx, before, "Deleted agent", nil, nil)
}

func (s *Store) Definitions(ctx context.Context) ([]json.RawMessage, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM agent_definitions ORDER BY agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(data))
	}
	return out, rows.Err()
}

func (s *Store) Definition(ctx context.Context, id string) (json.RawMessage, error) {
	var data string
	err := s.DB.QueryRowContext(ctx, `SELECT data FROM agent_definitions WHERE agent_id=?`, id).Scan(&data)
	return json.RawMessage(data), err
}

func (s *Store) Profile(ctx context.Context, id string) (contract.AgentProfile, error) {
	var profile contract.AgentProfile
	var data string
	err := s.DB.QueryRowContext(ctx, `SELECT a.data,json_extract(d.data,'$.prompt') FROM agents a JOIN agent_definitions d ON d.agent_id=a.id WHERE a.id=?`, id).Scan(&data, &profile.Prompt)
	if err != nil {
		return profile, err
	}
	err = json.Unmarshal([]byte(data), &profile.Agent)
	return profile, err
}

func (s *Store) UpdateDefinition(ctx context.Context, a contract.Agent, definition json.RawMessage) (contract.Agent, error) {
	if !json.Valid(definition) {
		return a, fmt.Errorf("invalid agent definition")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE agent_definitions SET data=? WHERE agent_id=?`, string(definition), a.ID)
	if err != nil {
		return a, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return a, err
	}
	if count == 0 {
		return a, sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET data=json_set(data,'$.name',?,'$.role',?) WHERE id=?`, a.Name, a.Role, a.ID); err != nil {
		return a, err
	}
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM agents WHERE id=?`, a.ID).Scan(&data); err != nil {
		return a, err
	}
	if err := json.Unmarshal([]byte(data), &a); err != nil {
		return a, err
	}
	if avatar := contract.GrokBotAvatarFromDefinition(definition); avatar != nil {
		a.Avatar = avatar
		agentData, err := json.Marshal(a)
		if err != nil {
			return a, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET data=? WHERE id=?`, string(agentData), a.ID); err != nil {
			return a, err
		}
	}
	return a, tx.Commit()
}

func (s *Store) UpdateAvatar(ctx context.Context, id string, avatar contract.AvatarDesign) (contract.Agent, error) {
	var agent contract.Agent
	data, err := json.Marshal(avatar)
	if err != nil {
		return agent, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return agent, err
	}
	defer tx.Rollback()
	var updated string
	if err := tx.QueryRowContext(ctx, `UPDATE agents SET data=json_set(data,'$.avatar',json(?)) WHERE id=? RETURNING data`, string(data), id).Scan(&updated); err != nil {
		return agent, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_definitions SET data=json_set(data,'$.agent.avatar',json(?)) WHERE agent_id=?`, string(data), id)
	if err != nil {
		return agent, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return agent, err
	}
	if count == 0 {
		return agent, sql.ErrNoRows
	}
	if err := json.Unmarshal([]byte(updated), &agent); err != nil {
		return agent, err
	}
	return agent, tx.Commit()
}

func (s *Store) Register(ctx context.Context, agents []contract.Agent) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, a := range agents {
		a.Active = false
		data, err := json.Marshal(a)
		if err != nil {
			return err
		}
		var deleted int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM deleted_agents WHERE id=?`, a.ID).Scan(&deleted); err != nil {
			return err
		}
		if deleted != 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO agents(id,data) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, a.ID, string(data)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO threads(id,agent_id) VALUES(?,?)`, a.ID, a.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO thread_members(thread_id,agent_id,position) VALUES(?,?,0)`, a.ID, a.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Agents(ctx context.Context) ([]contract.Agent, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM agents ORDER BY CASE id WHEN 'assistant' THEN 0 ELSE 1 END,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contract.Agent{}
	for rows.Next() {
		var data string
		var a contract.Agent
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Threads(ctx context.Context) ([]contract.Thread, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id,t.agent_id,t.last_read_seq,t.last_seq,
 (SELECT count(*) FROM messages m WHERE m.thread_id=t.id AND m.role='assistant' AND m.seq>t.last_read_seq
  AND NOT EXISTS(SELECT 1 FROM events e,json_each(e.data,'$.payload.messageIDs') hidden
   WHERE e.thread_id=t.id AND json_extract(e.data,'$.type')='activity' AND hidden.value=m.id))
 + (SELECT count(*) FROM events e WHERE e.thread_id=t.id AND e.seq>t.last_read_seq
    AND json_extract(e.data,'$.type')='activity'
    AND json_extract(e.data,'$.payload.kind') IN ('agent.messaged','subagent.finished')),
 (SELECT data FROM messages m WHERE m.thread_id=t.id ORDER BY seq DESC LIMIT 1),
	 COALESCE(NULLIF(t.title,''),json_extract(a.data,'$.name')),
	 (SELECT json_group_array(agent_id) FROM (SELECT agent_id FROM thread_members WHERE thread_id=t.id ORDER BY position)),
	 t.pinned,t.pin_order,t.section_id,t.section_order
	 FROM threads t JOIN agents a ON a.id=t.agent_id
	 ORDER BY t.pinned DESC,t.pin_order,CASE t.id WHEN 'assistant' THEN 0 ELSE 1 END,t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contract.Thread{}
	for rows.Next() {
		var t contract.Thread
		var last sql.NullString
		var members string
		if err := rows.Scan(&t.ID, &t.AgentID, &t.LastReadSeq, &t.LastSeq, &t.UnreadCount, &last, &t.Title, &members, &t.Pinned, &t.PinOrder, &t.SectionID, &t.SectionOrder); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(members), &t.AgentIDs); err != nil {
			return nil, err
		}
		if last.Valid {
			var m contract.Message
			if err := json.Unmarshal([]byte(last.String), &m); err != nil {
				return nil, err
			}
			t.LastMessage = &m
		}
		out = append(out, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		var lead string
		if err := s.DB.QueryRowContext(ctx, `SELECT lead_agent_id FROM threads WHERE id=?`, out[i].ID).Scan(&lead); err != nil {
			return nil, err
		}
		if lead == "" {
			lead = out[i].AgentID
		}
		out[i].LeadAgentID = &lead
		out[i].Runs, err = s.Runs(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) Thread(ctx context.Context, id string) (contract.Thread, error) {
	var t contract.Thread
	var members string
	err := s.DB.QueryRowContext(ctx, `SELECT t.id,t.agent_id,t.last_read_seq,t.last_seq,COALESCE(NULLIF(t.title,''),json_extract(a.data,'$.name')),(SELECT json_group_array(agent_id) FROM (SELECT agent_id FROM thread_members WHERE thread_id=t.id ORDER BY position)),t.pinned,t.pin_order,t.section_id,t.section_order FROM threads t JOIN agents a ON a.id=t.agent_id WHERE t.id=?`, id).Scan(&t.ID, &t.AgentID, &t.LastReadSeq, &t.LastSeq, &t.Title, &members, &t.Pinned, &t.PinOrder, &t.SectionID, &t.SectionOrder)
	if err == nil {
		err = json.Unmarshal([]byte(members), &t.AgentIDs)
	}
	if err == nil {
		var lead string
		err = s.DB.QueryRowContext(ctx, `SELECT lead_agent_id FROM threads WHERE id=?`, id).Scan(&lead)
		if lead == "" {
			lead = t.AgentID
		}
		t.LeadAgentID = &lead
	}
	if err == nil {
		t.Runs, err = s.Runs(ctx, id)
	}
	return t, err
}

func (s *Store) Read(ctx context.Context, id string, seq int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE threads SET last_read_seq=max(last_read_seq,min(last_seq,?)) WHERE id=?`, seq, id)
	return err
}

func appendTx(ctx context.Context, tx *sql.Tx, event contract.Event, message *contract.Message) (contract.Event, error) {
	newRun := false
	if message != nil && message.Status == "streaming" && event.RunID != nil {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM runs WHERE id=?`, *event.RunID).Scan(&status)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return event, err
		}
		if Terminal(status) {
			return event, context.Canceled
		}
	}
	if event.Run != nil {
		var data string
		err := tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=?`, event.Run.ID).Scan(&data)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return event, err
		}
		newRun = errors.Is(err, sql.ErrNoRows)
		if err == nil {
			var previous contract.Run
			if err := json.Unmarshal([]byte(data), &previous); err != nil {
				return event, err
			}
			if previous.ConversationID != event.Run.ConversationID || previous.AgentID != event.Run.AgentID {
				return event, ErrConflict
			}
			if Terminal(previous.Status) {
				if previous.Status != event.Run.Status {
					return event, errors.New("cannot reopen a terminal run")
				}
				*event.Run = previous
				event.Seq = previous.Seq
				return event, nil
			}
		}
		if event.Run.Status == "completed" {
			if err := resolvePeers(ctx, tx, event.Run); err != nil {
				return event, err
			}
			if err := resolveApprovals(ctx, tx, event.Run); err != nil {
				return event, err
			}
			if event.Run.Status == "waiting_peer" || event.Run.Status == "waiting_input" {
				event.Type = "run.update"
			}
		}
	}
	if event.CreatedAt == "" {
		event.CreatedAt = Now()
	}
	if err := tx.QueryRowContext(ctx, `UPDATE threads SET last_seq=last_seq+1 WHERE id=? RETURNING last_seq`, event.ConversationID).Scan(&event.Seq); err != nil {
		return event, err
	}
	if message != nil {
		var initial int64
		err := tx.QueryRowContext(ctx, `SELECT seq FROM messages WHERE id=?`, message.ID).Scan(&initial)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return event, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			initial = event.Seq
		}
		message.Seq = initial
		event.Message = message
		data, err := json.Marshal(message)
		if err != nil {
			return event, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO messages(id,thread_id,seq,role,status,data) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,data=excluded.data,indexing_state='pending'`, message.ID, message.ThreadID, message.Seq, message.Role, message.Status, string(data))
		if err != nil {
			return event, err
		}
	}
	if event.Run != nil {
		event.Run.Seq = event.Seq
		event.Run.UpdatedAt = event.CreatedAt
		data, err := json.Marshal(event.Run)
		if err != nil {
			return event, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO runs(id,job_id,thread_id,agent_id,status,data) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,data=excluded.data`, event.Run.ID, event.Run.JobID, event.Run.ConversationID, event.Run.AgentID, event.Run.Status, string(data)); err != nil {
			return event, err
		}
	}
	data, err := json.Marshal(event)
	if err != nil {
		return event, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO events(thread_id,seq,data) VALUES(?,?,?)`, event.ConversationID, event.Seq, string(data))
	if err == nil {
		err = noticeTx(ctx, tx, message, event)
	}
	if err == nil && event.Run != nil && event.Run.ParentRunID != nil && (event.Type == "run.start" && newRun || event.Type == "run.done") {
		err = childActivity(ctx, tx, event)
	}
	if err == nil && event.Run != nil && endsChildren(event.Run.Status) {
		err = cancelDescendants(ctx, tx, event.Run.ID)
	}
	if err == nil && event.Run != nil && Terminal(event.Run.Status) {
		err = releaseSteers(ctx, tx, event.Run.ID)
	}
	return event, err
}

func (s *Store) Append(ctx context.Context, event contract.Event, message *contract.Message) (contract.Event, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return event, err
	}
	defer tx.Rollback()
	event, err = appendTx(ctx, tx, event, message)
	if err != nil {
		return event, err
	}
	return event, tx.Commit()
}

func (s *Store) Presence(ctx context.Context, id string, active bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM agents WHERE id=?`, id).Scan(&data); err != nil {
		return err
	}
	var a contract.Agent
	if err := json.Unmarshal([]byte(data), &a); err != nil {
		return err
	}
	a.Active = active
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET data=? WHERE id=?`, string(b), id); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT thread_id FROM thread_members WHERE agent_id=?`, id)
	if err != nil {
		return err
	}
	var threads []string
	for rows.Next() {
		var thread string
		if err := rows.Scan(&thread); err != nil {
			rows.Close()
			return err
		}
		threads = append(threads, thread)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, thread := range threads {
		if _, err := appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: "agent.presence", AgentID: &id, Active: &active}, nil); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Events(ctx context.Context, id string, after int64, limit int) ([]contract.Event, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM events WHERE thread_id=? AND seq>? ORDER BY seq LIMIT ?`, id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contract.Event{}
	for rows.Next() {
		var data string
		var e contract.Event
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) Message(ctx context.Context, id string) (contract.Message, error) {
	var data string
	var m contract.Message
	err := s.DB.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=?`, id).Scan(&data)
	if err != nil {
		return m, err
	}
	err = json.Unmarshal([]byte(data), &m)
	return m, err
}

func (s *Store) Messages(ctx context.Context, id string, before int64, limit int) ([]contract.Message, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM (SELECT data,seq FROM messages WHERE thread_id=? AND seq<? ORDER BY seq DESC LIMIT ?) ORDER BY seq`, id, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contract.Message{}
	for rows.Next() {
		var data string
		var m contract.Message
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

var ErrConflict = errors.New("message id already used for different content")
var ErrAttachment = errors.New("attachment not found")
var ErrReply = errors.New("reply target not found in this conversation")

func (s *Store) Send(ctx context.Context, thread string, request contract.SendRequest, parts []contract.Part, targets ...string) (contract.Message, bool, error) {
	return s.Accept(ctx, SendInput{ThreadID: thread, Request: request, Parts: parts, Targets: targets})
}

func (s *Store) Accept(ctx context.Context, input SendInput) (contract.Message, bool, error) {
	thread, request, parts, targets := input.ThreadID, input.Request, input.Parts, input.Targets
	var m contract.Message
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return m, false, err
	}
	defer tx.Rollback()
	encoded, err := json.Marshal(request)
	if err != nil {
		return m, false, err
	}
	var previous, data string
	err = tx.QueryRowContext(ctx, `SELECT j.request,m.data FROM jobs j JOIN messages m ON m.id=j.id WHERE j.id=?`, request.ID).Scan(&previous, &data)
	if err == nil {
		if previous != string(encoded) {
			return m, false, ErrConflict
		}
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return m, false, err
		}
		if m.ThreadID != thread {
			return m, false, ErrConflict
		}
		return m, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return m, false, err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE id=?`, request.ID).Scan(&exists); err != nil {
		return m, false, err
	}
	if exists > 0 {
		return m, false, ErrConflict
	}
	for _, id := range request.AttachmentIDs {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM attachments WHERE id=?`, id).Scan(&exists); err != nil {
			return m, false, err
		}
		if exists == 0 {
			return m, false, ErrAttachment
		}
	}
	var reply *contract.ReplyContext
	if request.ReplyPart != nil && request.ReplyToID == nil {
		return m, false, ErrReply
	}
	if request.ReplyToID != nil {
		if *request.ReplyToID == "" || len(*request.ReplyToID) > 128 {
			return m, false, ErrReply
		}
		var target contract.Message
		if err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=? AND thread_id=?`, *request.ReplyToID, thread).Scan(&data); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return m, false, ErrReply
			}
			return m, false, fmt.Errorf("load reply target: %w", err)
		}
		if err := json.Unmarshal([]byte(data), &target); err != nil {
			return m, false, fmt.Errorf("decode reply target: %w", err)
		}
		reply = &contract.ReplyContext{MessageID: target.ID, Role: target.Role, AgentID: target.AgentID}
		if request.ReplyPart != nil {
			index := *request.ReplyPart
			if index < 0 || index >= int64(len(target.Parts)) {
				return m, false, ErrReply
			}
			part := target.Parts[index]
			reply.PartIndex = &index
			switch part.Kind {
			case "text":
				if part.Text == nil || *part.Text == "" {
					return m, false, ErrReply
				}
				reply.Text = *part.Text
			case "attachment":
				if part.Attachment == nil {
					return m, false, ErrReply
				}
				reply.Attachment = part.Attachment
			default:
				return m, false, ErrReply
			}
		} else {
			var texts []string
			for _, part := range target.Parts {
				if part.Text != nil {
					texts = append(texts, *part.Text)
				}
				if part.Attachment != nil && reply.Attachment == nil {
					reply.Attachment = part.Attachment
				}
			}
			reply.Text = strings.Join(texts, "\n")
		}
	}
	m = contract.Message{ID: request.ID, ThreadID: thread, Role: "user", Parts: parts, CreatedAt: Now(), Status: "done", ReplyToID: request.ReplyToID, Reply: reply, Mentions: request.Mentions}
	if _, err := appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: "message.start", MessageID: &m.ID}, &m); err != nil {
		return m, false, err
	}
	if _, err := appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: "message.done", MessageID: &m.ID}, &m); err != nil {
		return m, false, err
	}
	var parent *string
	if len(targets) == 1 {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id FROM runs WHERE thread_id=? AND agent_id=? AND status='running' ORDER BY rowid DESC LIMIT 1`, thread, targets[0]).Scan(&id)
		if err == nil {
			parent = &id
		} else if !errors.Is(err, sql.ErrNoRows) {
			return m, false, err
		}
	}
	work, err := json.Marshal(Work{Trigger: "message", Message: m, Targets: targets, Routing: input.Routing, ParentRunID: parent})
	if err != nil {
		return m, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,thread_id,request,input) VALUES(?,?,?,?)`, request.ID, thread, string(encoded), string(work)); err != nil {
		return m, false, err
	}
	if parent != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state='steer' WHERE id=?`, request.ID); err != nil {
			return m, false, err
		}
		return m, true, tx.Commit()
	}
	for _, target := range targets {
		run := NewRun(request.ID, thread, target, "message")
		run.SourceMessageID = &m.ID
		if _, err := appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: "run.start", AgentID: &target, RunID: &run.ID, Run: &run}, nil); err != nil {
			return m, false, err
		}
	}
	if thread == m.ThreadID && len(targets) == 1 && targets[0] == thread {
		if err := activateReviews(ctx, tx, thread, request.Text); err != nil {
			return m, false, err
		}
	}
	return m, true, tx.Commit()
}

func (s *Store) Job(ctx context.Context, thread string) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `UPDATE jobs SET state='running' WHERE id=(SELECT id FROM jobs WHERE thread_id=? AND state='queued' ORDER BY rowid LIMIT 1) RETURNING id`, thread).Scan(&id)
	return id, err
}

func (s *Store) Finish(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state='done' WHERE id=?`, id); err != nil {
		return err
	}
	if err := finishRuns(ctx, tx, id); err != nil {
		return err
	}
	if err := finishHandoffs(ctx, tx, id); err != nil {
		return err
	}
	if err := finishPeer(ctx, tx, id); err != nil {
		return err
	}
	if err := resumeJobTx(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Recover(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE push_queue SET state='unknown' WHERE state='sending'`); err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM messages WHERE status='streaming'`)
	if err != nil {
		return err
	}
	var interrupted []contract.Message
	for rows.Next() {
		var data string
		var m contract.Message
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			rows.Close()
			return err
		}
		interrupted = append(interrupted, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, m := range interrupted {
		m.Status = "error"
		if _, err := s.Append(ctx, contract.Event{ConversationID: m.ThreadID, Type: "message.error", MessageID: &m.ID, Error: &contract.Error{Code: "interrupted", Message: "The server restarted during this reply. Send another message to continue."}}, &m); err != nil {
			return err
		}
	}
	if err := s.RecoverTasks(ctx); err != nil {
		return err
	}
	return s.recoverRuns(ctx)
}

func (s *Store) SaveAttachment(ctx context.Context, a contract.Attachment, path string) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO attachments(id,data,path) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, a.ID, string(b), path)
	return err
}

func (s *Store) UpdateAttachment(ctx context.Context, a contract.Attachment) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE attachments SET data=? WHERE id=?`, string(b), a.ID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) Attachment(ctx context.Context, id string) (contract.Attachment, string, error) {
	var a contract.Attachment
	var data, path string
	err := s.DB.QueryRowContext(ctx, `SELECT data,path FROM attachments WHERE id=?`, id).Scan(&data, &path)
	if err != nil {
		return a, path, err
	}
	err = json.Unmarshal([]byte(data), &a)
	return a, path, err
}

func (s *Store) Session(ctx context.Context, id string) (string, error) {
	var path string
	err := s.DB.QueryRowContext(ctx, `SELECT path FROM sessions WHERE agent_id=?`, id).Scan(&path)
	return path, err
}
func (s *Store) SaveSession(ctx context.Context, id, path string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(agent_id,path) VALUES(?,?) ON CONFLICT(agent_id) DO UPDATE SET path=excluded.path`, id, path)
	return err
}

func Text(m contract.Message) string {
	var parts []string
	for _, p := range m.Parts {
		if p.Text != nil {
			parts = append(parts, *p.Text)
		}
		if p.Attachment != nil {
			parts = append(parts, p.Attachment.Name)
			if p.Attachment.ExtractedText != nil {
				parts = append(parts, *p.Attachment.ExtractedText)
			}
		}
	}
	return strings.Join(parts, "\n")
}
