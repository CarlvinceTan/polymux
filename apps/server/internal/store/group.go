package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	thread = contract.Thread{ID: id, AgentID: request.AgentIDs[0], AgentIDs: request.AgentIDs, Title: title}
	lead := thread.AgentID
	if request.LeadAgentID != nil {
		lead = *request.LeadAgentID
	}
	if !slices.Contains(request.AgentIDs, lead) {
		return thread, ErrMembers
	}
	thread.LeadAgentID = &lead
	if _, err := tx.ExecContext(ctx, `INSERT INTO threads(id,agent_id,title,lead_agent_id) VALUES(?,?,?,?)`, thread.ID, thread.AgentID, title, lead); err != nil {
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
	return thread, sidebarCommit(ctx, tx, before, "Created "+thread.Title, nil, nil)
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
	var primary, lead, title string
	if err := tx.QueryRowContext(ctx, `SELECT agent_id,lead_agent_id,title FROM threads WHERE id=?`, id).Scan(&primary, &lead, &title); err != nil {
		return err
	}
	if request.Title != nil {
		title = *request.Title
	}
	if request.AgentIDs != nil {
		if primary == id {
			return ErrMembers
		}
		if _, err := memberNames(ctx, tx, request.AgentIDs, 1); err != nil {
			return err
		}
		primary = request.AgentIDs[0]
		if !slices.Contains(request.AgentIDs, lead) {
			lead = primary
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM thread_members WHERE thread_id=?`, id); err != nil {
			return err
		}
		for position, agentID := range request.AgentIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO thread_members(thread_id,agent_id,position) VALUES(?,?,?)`, id, agentID, position); err != nil {
				return err
			}
		}
	}
	if request.LeadAgentID != nil {
		lead = *request.LeadAgentID
	}
	if lead == "" {
		lead = primary
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM thread_members WHERE thread_id=? AND agent_id=?`, id, lead).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return ErrMembers
	}
	if err := updatePin(ctx, tx, id, request.Pinned, request.PinOrder); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET title=?,agent_id=?,lead_agent_id=? WHERE id=?`, title, primary, lead, id); err != nil {
		return err
	}
	return sidebarCommit(ctx, tx, before, "Updated chat placement", nil, nil)
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
