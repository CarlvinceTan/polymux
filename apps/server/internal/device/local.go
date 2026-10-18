package device

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

// Local provisions the owner app without granting native control capabilities.
func (s *Store) Local(ctx context.Context, dir string) (string, error) {
	path := filepath.Join(dir, "devices", "local-token")
	data, err := os.ReadFile(path)
	if err == nil {
		token := string(data)
		if _, authErr := s.Authenticate(ctx, token); authErr == nil {
			return token, os.Chmod(path, 0600)
		} else {
			var revoked bool
			if err := s.db.QueryRowContext(ctx, `SELECT revoked FROM devices WHERE token_hash=? AND agent='local'`, hash(token)).Scan(&revoked); err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return "", err
				}
			} else if !revoked {
				return "", authErr
			}
		}
		// A revoked owner credential must not prevent supervised startup.
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	token := randomID()
	_, err = s.db.ExecContext(ctx, `INSERT INTO devices(id,name,agent,capabilities,token_hash) VALUES(?,?,?,?,?)`, randomID(), "Local Mac app", "local", "[]", hash(token))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		return "", err
	}
	return token, nil
}
