package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

var ErrSectionName = errors.New("invalid section name")
var ErrSectionLimit = errors.New("section limit reached")
var ErrSectionTaken = errors.New("section name already exists")
var ErrPlacement = errors.New("invalid chat placement")
var ErrSectionMissing = fmt.Errorf("section not found: %w", ErrPlacement)
var ErrChatPinned = fmt.Errorf("chat is pinned: %w", ErrPlacement)
var ErrAnchor = errors.New("invalid sidebar anchor")

func (s *Store) migrateSections(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sections(id TEXT PRIMARY KEY,name TEXT NOT NULL,position REAL NOT NULL,collapsed INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS sections_name ON sections(name COLLATE NOCASE);
CREATE TABLE IF NOT EXISTS sidebar_sources(source TEXT PRIMARY KEY,data TEXT NOT NULL,updated_at TEXT NOT NULL);`); err != nil {
		return err
	}
	for _, column := range []struct{ name, statement string }{
		{"section_id", `ALTER TABLE threads ADD COLUMN section_id TEXT REFERENCES sections(id) ON DELETE SET NULL`},
		{"section_order", `ALTER TABLE threads ADD COLUMN section_order REAL`},
	} {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('threads') WHERE name=?`, column.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := tx.ExecContext(ctx, column.statement); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS threads_section ON threads(section_id,section_order)`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func sectionName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 40 || strings.ContainsAny(name, "\r\n\x00") {
		return "", ErrSectionName
	}
	return name, nil
}

const sectionSelect = `SELECT s.id,s.name,s.position,s.collapsed,s.created_at,s.updated_at,(SELECT count(*) FROM threads t WHERE t.section_id=s.id) FROM sections s`

func sectionScan(row interface{ Scan(...any) error }) (contract.Section, error) {
	var section contract.Section
	err := row.Scan(&section.ID, &section.Name, &section.Order, &section.Collapsed, &section.CreatedAt, &section.UpdatedAt, &section.ChatCount)
	return section, err
}

func (s *Store) Sections(ctx context.Context) ([]contract.Section, error) {
	rows, err := s.DB.QueryContext(ctx, sectionSelect+` ORDER BY s.position,s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sections := []contract.Section{}
	for rows.Next() {
		section, err := sectionScan(rows)
		if err != nil {
			return nil, err
		}
		sections = append(sections, section)
	}
	return sections, rows.Err()
}

func (s *Store) CreateSection(ctx context.Context, id, name string, after *string) (contract.Section, bool, error) {
	name, err := sectionName(name)
	if err != nil {
		return contract.Section{}, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return contract.Section{}, false, err
	}
	defer tx.Rollback()
	before, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return contract.Section{}, false, err
	}
	old, err := sectionScan(tx.QueryRowContext(ctx, sectionSelect+` WHERE s.name=? COLLATE NOCASE`, name))
	if err == nil {
		return old, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return old, false, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sections`).Scan(&count); err != nil {
		return old, false, err
	}
	if count >= 30 {
		return old, false, ErrSectionLimit
	}
	now := Now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO sections(id,name,position,created_at,updated_at) VALUES(?,?,(SELECT COALESCE(max(position)+1,0) FROM sections),?,?)`, id, name, now, now); err != nil {
		return old, false, err
	}
	if after != nil {
		if err := reorderTx(ctx, tx, SidebarOrder{Kind: "sections", ID: id, AfterID: after}); err != nil {
			return old, false, err
		}
	}
	section, err := sectionScan(tx.QueryRowContext(ctx, sectionSelect+` WHERE s.id=?`, id))
	if err != nil {
		return old, false, err
	}
	return section, true, sidebarCommit(ctx, tx, before, "Created section 📁 "+section.Name, nil, nil)
}

func (s *Store) UpdateSection(ctx context.Context, id string, name *string, collapsed *bool) (contract.Section, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return contract.Section{}, err
	}
	defer tx.Rollback()
	before, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return contract.Section{}, err
	}
	section, err := sectionScan(tx.QueryRowContext(ctx, sectionSelect+` WHERE s.id=?`, id))
	if err != nil {
		return section, err
	}
	oldName, oldCollapsed := section.Name, section.Collapsed
	if name != nil {
		section.Name, err = sectionName(*name)
		if err != nil {
			return section, err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sections WHERE id<>? AND name=? COLLATE NOCASE`, id, section.Name).Scan(&count); err != nil {
			return section, err
		}
		if count != 0 {
			return section, ErrSectionTaken
		}
	}
	if collapsed != nil {
		section.Collapsed = *collapsed
	}
	if oldName == section.Name && oldCollapsed == section.Collapsed {
		return section, nil
	}
	section.UpdatedAt = Now()
	if _, err := tx.ExecContext(ctx, `UPDATE sections SET name=?,collapsed=?,updated_at=? WHERE id=?`, section.Name, section.Collapsed, section.UpdatedAt, id); err != nil {
		return section, err
	}
	label := "Updated section 📁 " + section.Name
	if oldName != section.Name {
		label = "Renamed section 📁 " + oldName + " → " + section.Name
	}
	return section, sidebarCommit(ctx, tx, before, label, nil, nil)
}

func (s *Store) DeleteSection(ctx context.Context, id string) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	before, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return nil, err
	}
	section, err := sectionScan(tx.QueryRowContext(ctx, sectionSelect+` WHERE s.id=?`, id))
	if err != nil {
		return nil, err
	}
	ids, err := sidebarIDs(ctx, tx, `SELECT id FROM threads WHERE section_id=? ORDER BY section_order,id`, id)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET section_id=NULL,section_order=NULL WHERE section_id=?`, id); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sections WHERE id=?`, id); err != nil {
		return nil, err
	}
	return ids, sidebarCommit(ctx, tx, before, fmt.Sprintf("Deleted section 📁 %s · %d chats returned", section.Name, len(ids)), &section, ids)
}

type Placement struct {
	Pinned     *bool
	SectionID  *string
	SectionSet bool
	BeforeID   *string
	AfterID    *string
}

type SidebarOrder struct {
	Kind      string  `json:"kind"`
	ID        string  `json:"id"`
	SectionID *string `json:"sectionID,omitempty"`
	BeforeID  *string `json:"beforeID,omitempty"`
	AfterID   *string `json:"afterID,omitempty"`
}

func (s *Store) PlaceThread(ctx context.Context, id string, request Placement) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return err
	}
	var pinned bool
	var section *string
	if err := tx.QueryRowContext(ctx, `SELECT pinned,section_id FROM threads WHERE id=?`, id).Scan(&pinned, &section); err != nil {
		return err
	}
	if request.SectionSet && pinned && (request.Pinned == nil || *request.Pinned) {
		return ErrChatPinned
	}
	if request.SectionSet && request.SectionID != nil {
		if request.Pinned != nil && *request.Pinned {
			return ErrPlacement
		}
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sections WHERE id=?`, *request.SectionID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrSectionMissing
		}
	}
	if err := updatePin(ctx, tx, id, request.Pinned, nil); err != nil {
		return err
	}
	if request.Pinned != nil {
		pinned = *request.Pinned
	}
	if request.SectionSet && !pinned && ((section == nil) != (request.SectionID == nil) || (section != nil && request.SectionID != nil && *section != *request.SectionID)) {
		section = request.SectionID
		if _, err := tx.ExecContext(ctx, `UPDATE threads SET section_id=?,section_order=(SELECT COALESCE(max(section_order)+1,0) FROM threads WHERE section_id IS ?) WHERE id=?`, section, section, id); err != nil {
			return err
		}
	}
	if request.BeforeID != nil || request.AfterID != nil {
		if pinned {
			return ErrPlacement
		}
		if err := reorderTx(ctx, tx, SidebarOrder{Kind: "chats", ID: id, SectionID: section, BeforeID: request.BeforeID, AfterID: request.AfterID}); err != nil {
			return err
		}
	}
	title, err := sidebarTitle(ctx, tx, id)
	if err != nil {
		return err
	}
	label := "Reordered chats"
	if request.Pinned != nil {
		label = "Unpinned " + title
		if pinned {
			label = "Pinned " + title
		}
	} else if request.SectionSet {
		label = "Moved " + title + " to main list"
		if section != nil {
			value, err := sectionScan(tx.QueryRowContext(ctx, sectionSelect+` WHERE s.id=?`, *section))
			if err != nil {
				return err
			}
			label = "Moved " + title + " to " + value.Name
		}
	}
	return sidebarCommit(ctx, tx, before, label, nil, nil)
}

func (s *Store) ReorderSidebar(ctx context.Context, request SidebarOrder) (float64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	before, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := reorderTx(ctx, tx, request); err != nil {
		return 0, err
	}
	var order float64
	if request.Kind == "sections" {
		err = tx.QueryRowContext(ctx, `SELECT position FROM sections WHERE id=?`, request.ID).Scan(&order)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(section_order,0) FROM threads WHERE id=?`, request.ID).Scan(&order)
	}
	if err != nil {
		return 0, err
	}
	return order, sidebarCommit(ctx, tx, before, "Reordered "+request.Kind, nil, nil)
}

func sidebarIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func reorderTx(ctx context.Context, tx *sql.Tx, request SidebarOrder) error {
	if request.BeforeID != nil && request.AfterID != nil {
		return ErrAnchor
	}
	var ids []string
	var err error
	if request.Kind == "sections" {
		if request.SectionID != nil {
			return ErrAnchor
		}
		ids, err = sidebarIDs(ctx, tx, `SELECT id FROM sections ORDER BY position,id`)
	} else if request.Kind == "chats" {
		ids, err = sidebarIDs(ctx, tx, `SELECT id FROM threads WHERE pinned=0 AND section_id IS ? ORDER BY section_order,CASE id WHEN 'assistant' THEN 0 ELSE 1 END,id`, request.SectionID)
	} else {
		return ErrAnchor
	}
	if err != nil {
		return err
	}
	index := slices.Index(ids, request.ID)
	if index == -1 {
		return sql.ErrNoRows
	}
	previous := slices.Clone(ids)
	ids = slices.Delete(ids, index, index+1)
	index = len(ids)
	if request.BeforeID != nil {
		index = slices.Index(ids, *request.BeforeID)
		if index == -1 {
			return ErrAnchor
		}
	}
	if request.AfterID != nil {
		index = slices.Index(ids, *request.AfterID)
		if index == -1 {
			return ErrAnchor
		}
		index++
	}
	ids = slices.Insert(ids, index, request.ID)
	if slices.Equal(previous, ids) {
		return nil
	}
	for position, id := range ids {
		if request.Kind == "sections" {
			_, err = tx.ExecContext(ctx, `UPDATE sections SET position=?,updated_at=? WHERE id=?`, position, Now(), id)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE threads SET section_order=? WHERE id=?`, position, id)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
