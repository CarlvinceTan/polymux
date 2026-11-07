package store

import "context"

// Cleanup is a step of a deletion that outlives its commit; the server retries it until it succeeds.
type Cleanup struct{ Kind, Target string }

const (
	CleanupAgent      = "agent"      // A deleted agent's draft, memory, secrets, media and folder.
	CleanupAttachment = "attachment" // A deleted attachment's file path.
	CleanupDraft      = "draft"      // The draft of a group its last member left.
	CleanupGroup      = "group"      // A deleted group's draft and folder.
	CleanupMember     = "member"     // A deleted agent's folder in a group, as group/agent.
	CleanupTranscript = "transcript" // A finished conversation's durable session path.
)

func (s *Store) migrateCleanups(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cleanups(kind TEXT NOT NULL, target TEXT NOT NULL, PRIMARY KEY(kind,target))`)
	return err
}

// AddCleanup queues a step outside a deletion.
func (s *Store) AddCleanup(ctx context.Context, step Cleanup) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO cleanups(kind,target) VALUES(?,?)`, step.Kind, step.Target)
	return err
}

// SavedSessions lists every session path a conversation can be reopened from.
func (s *Store) SavedSessions(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT path FROM sessions UNION SELECT path FROM group_sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

// Cleanups lists unfinished steps, oldest first.
func (s *Store) Cleanups(ctx context.Context) ([]Cleanup, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT kind,target FROM cleanups ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Cleanup{}
	for rows.Next() {
		var step Cleanup
		if err := rows.Scan(&step.Kind, &step.Target); err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, rows.Err()
}

// Cleaned removes a finished step.
func (s *Store) Cleaned(ctx context.Context, step Cleanup) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM cleanups WHERE kind=? AND target=?`, step.Kind, step.Target)
	return err
}
