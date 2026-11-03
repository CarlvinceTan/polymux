package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func (s *Store) migrateGroups(ctx context.Context) (err error) {
	var version int
	if err = s.DB.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version >= 3 {
		return err
	}
	if _, err = s.DB.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer func() {
		_, restore := s.DB.ExecContext(ctx, `PRAGMA foreign_keys=ON`)
		err = errors.Join(err, restore)
	}()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
CREATE TABLE threads_next(id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id), title TEXT NOT NULL DEFAULT '', last_read_seq INTEGER NOT NULL DEFAULT 0, last_seq INTEGER NOT NULL DEFAULT 0);
INSERT INTO threads_next(id,agent_id,last_read_seq,last_seq) SELECT id,agent_id,last_read_seq,last_seq FROM threads;
DROP TABLE threads;
ALTER TABLE threads_next RENAME TO threads;
CREATE INDEX threads_agent ON threads(agent_id);
CREATE TABLE thread_members(thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE, agent_id TEXT NOT NULL REFERENCES agents(id), position INTEGER NOT NULL, PRIMARY KEY(thread_id,agent_id), UNIQUE(thread_id,position));
INSERT INTO thread_members(thread_id,agent_id,position) SELECT id,agent_id,0 FROM threads;
CREATE TABLE group_sessions(thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE, agent_id TEXT NOT NULL REFERENCES agents(id), path TEXT NOT NULL, PRIMARY KEY(thread_id,agent_id));
PRAGMA user_version=3;`)
	if err != nil {
		return fmt.Errorf("migrate group conversations: %w", err)
	}
	return tx.Commit()
}

var ErrMembers = errors.New("invalid group members")

func (s *Store) CreateThread(ctx context.Context, id string, request contract.CreateThreadRequest) (contract.Thread, error) {
	var thread contract.Thread
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return thread, err
	}
	defer tx.Rollback()
	before, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return thread, err
	}
	if request.SectionID != nil {
		if _, err := sectionScan(tx.QueryRowContext(ctx, sectionSelect+` WHERE s.id=?`, *request.SectionID)); errors.Is(err, sql.ErrNoRows) {
			return thread, ErrSectionMissing
		} else if err != nil {
			return thread, err
		}
	}
	names, err := memberNames(ctx, tx, request.AgentIDs, 2)
	if err != nil {
		return thread, err
	}
	title := strings.Join(names, ", ")
	if request.Title != nil && strings.TrimSpace(*request.Title) != "" {
		title = strings.TrimSpace(*request.Title)
	}
	thread = contract.Thread{ID: id, AgentID: request.AgentIDs[0], AgentIDs: request.AgentIDs, Title: title, Runs: []contract.Run{}}
	if _, err := tx.ExecContext(ctx, `INSERT INTO threads(id,agent_id,title) VALUES(?,?,?)`, thread.ID, thread.AgentID, title); err != nil {
		return thread, err
	}
	for position, agentID := range request.AgentIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO thread_members(thread_id,agent_id,position) VALUES(?,?,?)`, id, agentID, position); err != nil {
			return thread, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET section_id=?,section_order=(SELECT COALESCE(max(section_order)+1,0) FROM threads WHERE section_id IS ?) WHERE id=?`, request.SectionID, request.SectionID, id); err != nil {
		return thread, err
	}
	thread.SectionID = request.SectionID
	if err := tx.QueryRowContext(ctx, `SELECT section_order FROM threads WHERE id=?`, id).Scan(&thread.SectionOrder); err != nil {
		return thread, err
	}
	thread.Pinned = new(false)
	return thread, s.sidebarCommit(ctx, tx, before, "Created "+thread.Title, nil, nil)
}

func (s *Store) UpdateThread(ctx context.Context, id string, request contract.UpdateThreadRequest) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return err
	}
	var primary, title string
	if err := tx.QueryRowContext(ctx, `SELECT agent_id,title FROM threads WHERE id=?`, id).Scan(&primary, &title); err != nil {
		return err
	}
	if request.Title != nil {
		title = *request.Title
	}
	if request.AgentIDs != nil {
		var task bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM task_threads WHERE thread_id=?)`, id).Scan(&task); err != nil {
			return err
		}
		if primary == id || task {
			return ErrMembers
		}
		if _, err := memberNames(ctx, tx, request.AgentIDs, 1); err != nil {
			return err
		}
		primary = request.AgentIDs[0]
		if _, err := tx.ExecContext(ctx, `DELETE FROM thread_members WHERE thread_id=?`, id); err != nil {
			return err
		}
		for position, agentID := range request.AgentIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO thread_members(thread_id,agent_id,position) VALUES(?,?,?)`, id, agentID, position); err != nil {
				return err
			}
		}
	}
	if err := updatePin(ctx, tx, id, request.Pinned, request.PinOrder); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET title=?,agent_id=? WHERE id=?`, title, primary, id); err != nil {
		return err
	}
	return s.sidebarCommit(ctx, tx, before, "Updated chat placement", nil, nil)
}

func memberNames(ctx context.Context, tx *sql.Tx, ids []string, minimum int) ([]string, error) {
	if len(ids) < minimum || len(ids) > 16 {
		return nil, ErrMembers
	}
	seen := make(map[string]bool, len(ids))
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, ErrMembers
		}
		seen[id] = true
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT json_extract(data,'$.name') FROM agents WHERE id=?`, id).Scan(&name); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrMembers
			}
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

func (s *Store) GroupSession(ctx context.Context, thread, agent string) (string, error) {
	var path string
	err := s.DB.QueryRowContext(ctx, `SELECT path FROM group_sessions WHERE thread_id=? AND agent_id=?`, thread, agent).Scan(&path)
	return path, err
}

func (s *Store) SaveGroupSession(ctx context.Context, thread, agent, path string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO group_sessions(thread_id,agent_id,path) VALUES(?,?,?) ON CONFLICT(thread_id,agent_id) DO UPDATE SET path=excluded.path`, thread, agent, path)
	return err
}

func (s *Store) Request(ctx context.Context, id string) (contract.SendRequest, error) {
	var request contract.SendRequest
	var data string
	err := s.DB.QueryRowContext(ctx, `SELECT request FROM jobs WHERE id=?`, id).Scan(&data)
	if err != nil {
		return request, err
	}
	err = json.Unmarshal([]byte(data), &request)
	return request, err
}

// GroupMessages includes immediate posts, but not silent passes or unfinished replies.
func (s *Store) GroupMessages(ctx context.Context, job string) ([]contract.Message, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT m.data FROM messages m
JOIN runs r ON r.id=json_extract(m.data,'$.runID')
WHERE r.job_id=? AND m.role='assistant' AND m.status='done' ORDER BY m.seq`, job)
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
	return messages, rows.Err()
}

type GroupWork struct {
	Delivery contract.PeerDelivery
	Work     Work
}

// GroupQueue returns FIFO invitations whose source stage completed successfully.
func (s *Store) GroupQueue(ctx context.Context, thread, job string) ([]GroupWork, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.data,d.input FROM deliveries d
JOIN runs parent ON parent.id=json_extract(d.data,'$.parentRunID')
WHERE d.thread_id=? AND parent.status='completed'
AND json_extract(d.data,'$.intent')='handoff' AND json_extract(d.data,'$.status')='queued'
AND (json_extract(d.input,'$.message.replyToID')=? OR json_extract(d.input,'$.message.id')=?)
ORDER BY d.rowid`, thread, job, job)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	queue := []GroupWork{}
	for rows.Next() {
		var data, input string
		var item GroupWork
		if err := rows.Scan(&data, &input); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &item.Delivery); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(input), &item.Work); err != nil {
			return nil, err
		}
		queue = append(queue, item)
	}
	return queue, rows.Err()
}

// ErrNotGroup rejects deleting a bot's own chat as a group; delete the bot instead.
var ErrNotGroup = errors.New("not a group chat")

// DeleteGroup removes a group chat and the attachments only it references, queueing their files.
// Its members keep their own chats.
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return err
	}
	var agent string
	if err := tx.QueryRowContext(ctx, `SELECT agent_id FROM threads WHERE id=?`, id).Scan(&agent); err != nil {
		return err
	}
	if agent == id {
		return ErrNotGroup
	}
	if _, err := s.closePeers(ctx, tx, `SELECT data,input FROM deliveries WHERE thread_id=? AND json_extract(data,'$.status')='queued'`, id); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.path FROM attachments a
WHERE EXISTS(SELECT 1 FROM messages m, json_each(m.data,'$.parts') p WHERE m.thread_id=? AND json_extract(p.value,'$.attachment.id')=a.id)
AND NOT EXISTS(SELECT 1 FROM messages m, json_each(m.data,'$.parts') p WHERE m.thread_id<>? AND json_extract(p.value,'$.attachment.id')=a.id)`, id, id)
	if err != nil {
		return err
	}
	var files, attachments []string
	for rows.Next() {
		var attachment, path string
		if err := rows.Scan(&attachment, &path); err != nil {
			rows.Close()
			return err
		}
		attachments = append(attachments, attachment)
		files = append(files, path)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Members, sessions, notices, deliveries and search entries cascade with the thread.
	for _, query := range []string{
		`INSERT OR IGNORE INTO cleanups(kind,target) VALUES('group',?)`,
		`INSERT OR IGNORE INTO cleanups(kind,target) SELECT 'transcript',path FROM group_sessions WHERE thread_id=?`,
		`DELETE FROM runs WHERE thread_id=?`,
		`DELETE FROM jobs WHERE thread_id=?`,
		`DELETE FROM media_vectors WHERE thread_id=?`,
		`DELETE FROM media_index WHERE message_id IN(SELECT id FROM messages WHERE thread_id=?)`,
		`DELETE FROM messages_fts WHERE thread_id=?`,
		`DELETE FROM events WHERE thread_id=?`,
		`DELETE FROM messages WHERE thread_id=?`,
		`DELETE FROM threads WHERE id=?`,
	} {
		if _, err := tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	for _, attachment := range attachments {
		if _, err := tx.ExecContext(ctx, `DELETE FROM attachments WHERE id=?`, attachment); err != nil {
			return err
		}
	}
	for _, file := range files {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO cleanups(kind,target) VALUES('attachment',?)`, file); err != nil {
			return err
		}
	}
	return s.sidebarCommit(ctx, tx, before, "Deleted chat", nil, nil)
}
