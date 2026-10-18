package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

var ErrSidebarRate = errors.New("sidebar mutation rate reached")

type sidebarKey struct{}
type SidebarChange struct{ Changed bool }
type sidebarActor struct {
	agent, run string
	change     *SidebarChange
}

func SidebarActor(ctx context.Context, agent, run string) (context.Context, *SidebarChange) {
	change := &SidebarChange{}
	return context.WithValue(ctx, sidebarKey{}, sidebarActor{agent: agent, run: run, change: change}), change
}

func (s *Store) migrateSidebar(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sidebar_events(seq INTEGER PRIMARY KEY AUTOINCREMENT,actor_id TEXT NOT NULL,at INTEGER NOT NULL,data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS sidebar_rate ON sidebar_events(actor_id,at);
CREATE TABLE IF NOT EXISTS sidebar_audit(seq INTEGER PRIMARY KEY REFERENCES sidebar_events(seq),actor_id TEXT NOT NULL,section TEXT NOT NULL,returned_ids TEXT NOT NULL,created_at TEXT NOT NULL);`)
	return err
}

func sidebarSnapshot(ctx context.Context, tx *sql.Tx) (contract.SidebarSnapshot, error) {
	state := contract.SidebarSnapshot{Sections: []contract.Section{}, Placements: []contract.SidebarPlacement{}}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(seq),0) FROM sidebar_events`).Scan(&state.Cursor); err != nil {
		return state, err
	}
	rows, err := tx.QueryContext(ctx, sectionSelect+` ORDER BY s.position,s.id`)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		section, err := sectionScan(rows)
		if err != nil {
			rows.Close()
			return state, err
		}
		state.Sections = append(state.Sections, section)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return state, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,pinned,pin_order,section_id,section_order FROM threads ORDER BY id`)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var value contract.SidebarPlacement
		if err := rows.Scan(&value.ThreadID, &value.Pinned, &value.PinOrder, &value.SectionID, &value.SectionOrder); err != nil {
			return state, err
		}
		state.Placements = append(state.Placements, value)
	}
	return state, rows.Err()
}

func (s *Store) Sidebar(ctx context.Context) (contract.SidebarSnapshot, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return contract.SidebarSnapshot{}, err
	}
	defer tx.Rollback()
	state, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return state, err
	}
	return state, tx.Commit()
}

func sameSections(a, b []contract.Section) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		left, right := a[index], b[index]
		left.CreatedAt, left.UpdatedAt = "", ""
		right.CreatedAt, right.UpdatedAt = "", ""
		if left != right {
			return false
		}
	}
	return true
}

func sidebarCommit(ctx context.Context, tx *sql.Tx, before contract.SidebarSnapshot, label string, deleted *contract.Section, returned []string) error {
	after, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return err
	}
	if sameSections(before.Sections, after.Sections) && reflect.DeepEqual(before.Placements, after.Placements) {
		return tx.Commit()
	}
	actor, _ := ctx.Value(sidebarKey{}).(sidebarActor)
	now := time.Now()
	if actor.agent != "" {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sidebar_events WHERE actor_id=? AND at>?`, actor.agent, now.Add(-time.Minute).UnixMilli()).Scan(&count); err != nil {
			return err
		}
		if count >= 30 {
			return ErrSidebarRate
		}
	}
	event := contract.SidebarEvent{Type: "sidebar.updated", Sections: after.Sections, ChangedThreadIDs: []string{}, Placements: []contract.SidebarPlacement{}, CreatedAt: now.UTC().Format(time.RFC3339Nano)}
	old := make(map[string]contract.SidebarPlacement, len(before.Placements))
	for _, placement := range before.Placements {
		old[placement.ThreadID] = placement
	}
	for _, placement := range after.Placements {
		if previous, ok := old[placement.ThreadID]; !ok || !reflect.DeepEqual(previous, placement) {
			event.ChangedThreadIDs = append(event.ChangedThreadIDs, placement.ThreadID)
			event.Placements = append(event.Placements, placement)
		}
		delete(old, placement.ThreadID)
	}
	for id := range old {
		event.ChangedThreadIDs = append(event.ChangedThreadIDs, id)
	}
	slices.Sort(event.ChangedThreadIDs)
	result, err := tx.ExecContext(ctx, `INSERT INTO sidebar_events(actor_id,at,data) VALUES(?,?,'')`, actor.agent, now.UnixMilli())
	if err != nil {
		return err
	}
	event.Seq, err = result.LastInsertId()
	if err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sidebar_events SET data=? WHERE seq=?`, string(data), event.Seq); err != nil {
		return err
	}
	if deleted != nil {
		section, err := json.Marshal(deleted)
		if err != nil {
			return err
		}
		ids, err := json.Marshal(returned)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sidebar_audit(seq,actor_id,section,returned_ids,created_at) VALUES(?,?,?,?,?)`, event.Seq, actor.agent, string(section), string(ids), event.CreatedAt); err != nil {
			return err
		}
	}
	if actor.agent != "" {
		activity := contract.Event{ConversationID: actor.agent, AgentID: &actor.agent, Type: "activity", Summary: &label}
		if actor.run != "" {
			activity.RunID = &actor.run
		}
		if _, err := appendTx(ctx, tx, activity, nil); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if actor.change != nil {
		actor.change.Changed = true
	}
	return nil
}

func (s *Store) SidebarEvents(ctx context.Context, after int64, limit int) ([]contract.SidebarEvent, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM sidebar_events WHERE seq>? ORDER BY seq LIMIT ?`, after, min(max(limit, 1), 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contract.SidebarEvent{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var event contract.SidebarEvent
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, fmt.Errorf("read sidebar event: %w", err)
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func sidebarTitle(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var title string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(t.title,''),json_extract(a.data,'$.name'),t.id) FROM threads t JOIN agents a ON a.id=t.agent_id WHERE t.id=?`, id).Scan(&title)
	return title, err
}
