package store

import (
	"context"
	"encoding/json"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func (s *Store) ConfigureAgent(ctx context.Context, id string, patch contract.ConfigurationPatch) (json.RawMessage, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM agent_definitions WHERE agent_id=?`, id).Scan(&raw); err != nil {
		return nil, err
	}
	if patch.Workspace != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE agent_definitions SET data=json_set(data,'$.workspace',?,'$.workspaceSet',json('true')) WHERE agent_id=?`, *patch.Workspace, id); err != nil {
			return nil, err
		}
	}
	for _, field := range []struct {
		values []string
		query  string
	}{
		{patch.Tools, `UPDATE agent_definitions SET data=json_set(data,'$.tools',json(?)) WHERE agent_id=?`},
		{patch.Skills, `UPDATE agent_definitions SET data=json_set(data,'$.skills',json(?)) WHERE agent_id=?`},
	} {
		if field.values != nil {
			data, err := json.Marshal(field.values)
			if err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, field.query, string(data), id); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT data FROM agent_definitions WHERE agent_id=?`, id).Scan(&raw); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), tx.Commit()
}
