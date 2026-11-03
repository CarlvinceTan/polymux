package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
)

var ErrProjectMissing = errors.New("no project with that slug exists; create it first")
var ErrProjectSlug = errors.New("project slug must be a short kebab-case id")

var projectSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Project is a named memory space that agents join to share its facts.
type Project struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members"`
}

func (s *Store) migrateProjects(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS projects(slug TEXT PRIMARY KEY,name TEXT NOT NULL,description TEXT NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS project_members(slug TEXT NOT NULL REFERENCES projects(slug),agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,PRIMARY KEY(slug,agent_id));`)
	return err
}

// ValidSlug reports whether slug can name a project.
func ValidSlug(slug string) bool { return len(slug) <= 64 && projectSlug.MatchString(slug) }

// CreateProject creates a project, or keeps an existing one, and joins the agent to it.
func (s *Store) CreateProject(ctx context.Context, agent, slug, name, description string) (created bool, err error) {
	if !ValidSlug(slug) {
		return false, ErrProjectSlug
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO projects(slug,name,description) VALUES(?,?,?)`, slug, name, description)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO project_members(slug,agent_id) VALUES(?,?)`, slug, agent); err != nil {
		return false, err
	}
	return rows == 1, tx.Commit()
}

func (s *Store) JoinProject(ctx context.Context, agent, slug string) error {
	var exists int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE slug=?`, slug).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrProjectMissing
	}
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO project_members(slug,agent_id) VALUES(?,?)`, slug, agent)
	return err
}

func (s *Store) LeaveProject(ctx context.Context, agent, slug string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM project_members WHERE slug=? AND agent_id=?`, slug, agent)
	return err
}

// Projects lists every project with its member agent IDs, by slug.
func (s *Store) Projects(ctx context.Context) ([]Project, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.slug,p.name,p.description,m.agent_id FROM projects p LEFT JOIN project_members m ON m.slug=p.slug ORDER BY p.slug,m.agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		var member sql.NullString
		if err := rows.Scan(&p.Slug, &p.Name, &p.Description, &member); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Slug != p.Slug {
			p.Members = []string{}
			out = append(out, p)
		}
		if member.Valid {
			last := &out[len(out)-1]
			last.Members = append(last.Members, member.String)
		}
	}
	return out, rows.Err()
}
