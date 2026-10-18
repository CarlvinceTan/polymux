package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

var ErrSidebarImport = errors.New("invalid sidebar import")

type SourceSection struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Collapsed *bool    `json:"collapsed"`
	ThreadIDs []string `json:"threadIDs"`
}

type SidebarImport struct {
	SchemaVersion   int             `json:"schemaVersion"`
	Source          string          `json:"source"`
	Sections        []SourceSection `json:"sections"`
	PinnedThreadIDs []string        `json:"pinnedThreadIDs"`
}

type SidebarReport struct {
	Applied       bool     `json:"applied"`
	Sections      int      `json:"sections"`
	Placements    int      `json:"placements"`
	Pins          int      `json:"pins"`
	UnresolvedIDs []string `json:"unresolvedIDs"`
}

func validateSidebar(source SidebarImport) error {
	if source.SchemaVersion != 1 || source.Source != "grokbot" || source.Sections == nil || source.PinnedThreadIDs == nil || len(source.Sections) > 31 || len(source.PinnedThreadIDs) > 12 {
		return ErrSidebarImport
	}
	seen := map[string]bool{}
	names := map[string]bool{}
	threads := map[string]bool{}
	for _, section := range source.Sections {
		name, err := sectionName(section.Name)
		if err != nil || name != section.Name || section.ID == "" || len(section.ID) > 128 || seen[section.ID] || section.ThreadIDs == nil || section.Collapsed == nil {
			return ErrSidebarImport
		}
		seen[section.ID] = true
		if section.ID != "__agents__" {
			key := strings.ToLower(name)
			if names[key] {
				return ErrSidebarImport
			}
			names[key] = true
		}
		for _, id := range section.ThreadIDs {
			if id == "" || len(id) > 128 || threads[id] {
				return ErrSidebarImport
			}
			threads[id] = true
		}
	}
	if len(names) > 30 || len(threads) > 10000 {
		return ErrSidebarImport
	}
	for _, id := range source.PinnedThreadIDs {
		if id == "" || len(id) > 128 || threads[id] {
			return ErrSidebarImport
		}
		threads[id] = true
	}
	return nil
}

func (s *Store) ImportSidebar(ctx context.Context, source SidebarImport, apply bool) (SidebarReport, error) {
	report := SidebarReport{UnresolvedIDs: []string{}}
	if err := validateSidebar(source); err != nil {
		return report, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	before, err := sidebarSnapshot(ctx, tx)
	if err != nil {
		return report, err
	}
	known, err := sidebarIDs(ctx, tx, `SELECT id FROM threads`)
	if err != nil {
		return report, err
	}
	threads := make(map[string]bool, len(known))
	for _, id := range known {
		threads[id] = true
	}
	var extra int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sections`).Scan(&extra); err != nil {
		return report, err
	}
	for _, section := range source.Sections {
		if section.ID == "__agents__" {
			continue
		}
		var current string
		err := tx.QueryRowContext(ctx, `SELECT id FROM sections WHERE name=? COLLATE NOCASE`, section.Name).Scan(&current)
		if err == nil && current != section.ID {
			return report, ErrSectionTaken
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return report, err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sections WHERE id=?`, section.ID).Scan(&count); err != nil {
			return report, err
		}
		if count == 0 {
			extra++
		}
	}
	if extra > 30 {
		return report, ErrSectionLimit
	}
	// Preserve unrelated pins after the explicit source pin order.
	pins, err := sidebarIDs(ctx, tx, `SELECT id FROM threads WHERE pinned=1 ORDER BY pin_order,id`)
	if err != nil {
		return report, err
	}
	mentioned := map[string]bool{}
	for _, section := range source.Sections {
		for _, id := range section.ThreadIDs {
			mentioned[id] = true
		}
	}
	for _, id := range source.PinnedThreadIDs {
		mentioned[id] = true
	}
	pinCount := 0
	for _, id := range source.PinnedThreadIDs {
		if threads[id] {
			pinCount++
		}
	}
	for _, id := range pins {
		if !mentioned[id] {
			pinCount++
		}
	}
	if pinCount > 12 {
		return report, ErrPinLimit
	}
	for position, section := range source.Sections {
		var sectionID *string
		if section.ID != "__agents__" {
			sectionID = &section.ID
			report.Sections++
			if apply {
				now := Now()
				if _, err := tx.ExecContext(ctx, `INSERT INTO sections(id,name,position,collapsed,created_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,position=excluded.position,collapsed=excluded.collapsed,updated_at=excluded.updated_at`, section.ID, section.Name, position, section.Collapsed, now, now); err != nil {
					return report, err
				}
			}
		}
		for order, id := range section.ThreadIDs {
			if !threads[id] {
				report.UnresolvedIDs = append(report.UnresolvedIDs, id)
				continue
			}
			report.Placements++
			if apply {
				if _, err := tx.ExecContext(ctx, `UPDATE threads SET pinned=0,pin_order=NULL,section_id=?,section_order=? WHERE id=?`, sectionID, order, id); err != nil {
					return report, err
				}
			}
		}
	}
	for order, id := range source.PinnedThreadIDs {
		if !threads[id] {
			report.UnresolvedIDs = append(report.UnresolvedIDs, id)
			continue
		}
		report.Pins++
		if apply {
			if _, err := tx.ExecContext(ctx, `UPDATE threads SET pinned=1,pin_order=?,section_id=NULL,section_order=NULL WHERE id=?`, order, id); err != nil {
				return report, err
			}
		}
	}
	if !apply {
		return report, nil
	}

	for _, section := range source.Sections {
		var sectionID *string
		if section.ID != "__agents__" {
			sectionID = &section.ID
		}
		ids, err := sidebarIDs(ctx, tx, `SELECT id FROM threads WHERE pinned=0 AND section_id IS ? ORDER BY section_order,id`, sectionID)
		if err != nil {
			return report, err
		}
		position := len(section.ThreadIDs)
		for _, id := range ids {
			if !mentioned[id] {
				if _, err := tx.ExecContext(ctx, `UPDATE threads SET section_order=? WHERE id=?`, position, id); err != nil {
					return report, err
				}
				position++
			}
		}
	}
	order := len(source.PinnedThreadIDs)
	for _, id := range pins {
		if !mentioned[id] {
			if _, err := tx.ExecContext(ctx, `UPDATE threads SET pin_order=? WHERE id=?`, order, id); err != nil {
				return report, err
			}
			order++
		}
	}

	imported := make(map[string]bool, len(source.Sections))
	for _, section := range source.Sections {
		imported[section.ID] = true
	}
	sections, err := sidebarIDs(ctx, tx, `SELECT id FROM sections ORDER BY position,id`)
	if err != nil {
		return report, err
	}
	position := len(source.Sections)
	for _, id := range sections {
		if !imported[id] {
			if _, err := tx.ExecContext(ctx, `UPDATE sections SET position=? WHERE id=?`, position, id); err != nil {
				return report, err
			}
			position++
		}
	}
	data, err := json.Marshal(source)
	if err != nil {
		return report, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sidebar_sources(source,data,updated_at) VALUES(?,?,?) ON CONFLICT(source) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at`, source.Source, string(data), Now()); err != nil {
		return report, err
	}
	if err := sidebarCommit(ctx, tx, before, "Imported sidebar layout", nil, nil); err != nil {
		return report, err
	}
	report.Applied = true
	return report, nil
}

func (s *Store) SidebarSource(ctx context.Context, source string) (SidebarImport, error) {
	var data string
	var result SidebarImport
	if err := s.DB.QueryRowContext(ctx, `SELECT data FROM sidebar_sources WHERE source=?`, source).Scan(&data); err != nil {
		return result, err
	}
	err := json.Unmarshal([]byte(data), &result)
	return result, err
}
