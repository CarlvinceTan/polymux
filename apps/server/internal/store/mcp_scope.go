package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

var ErrMCPHidden = errors.New("MCP connection is hidden from this agent")
var ErrMCPUnknown = errors.New("MCP connection not found")
var ErrMCPLimit = errors.New("MCP connection limit reached")

type MCPBinding struct {
	Name    string
	Enabled bool
	Hidden  bool
}
type MCPRecord struct {
	Name string
	Data json.RawMessage
}

func (s *Store) migrateMCP(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mcp_servers(name TEXT PRIMARY KEY,data TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS mcp_bindings(agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,name TEXT NOT NULL,enabled INTEGER NOT NULL DEFAULT 0,hidden INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(agent_id,name));`)
	return err
}

func (s *Store) MCPServers(ctx context.Context) ([]MCPRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT name,data FROM mcp_servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MCPRecord{}
	for rows.Next() {
		var name, data string
		if err := rows.Scan(&name, &data); err != nil {
			return nil, err
		}
		out = append(out, MCPRecord{Name: name, Data: json.RawMessage(data)})
	}
	return out, rows.Err()
}

func (s *Store) MCPBindings(ctx context.Context, id string) ([]MCPBinding, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT name,enabled,hidden FROM mcp_bindings WHERE agent_id=? ORDER BY name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MCPBinding{}
	for rows.Next() {
		var value MCPBinding
		if err := rows.Scan(&value.Name, &value.Enabled, &value.Hidden); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) RegisterMCP(ctx context.Context, name string, data json.RawMessage, owner string, local, devices json.RawMessage) error {
	if !json.Valid(data) || local != nil && !json.Valid(local) || devices != nil && !json.Valid(devices) {
		return ErrMCPUnknown
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM mcp_servers WHERE name<>?`, name).Scan(&count); err != nil {
		return err
	}
	if count >= 64 {
		return ErrMCPLimit
	}
	if owner != "" {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE id=?`, owner).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return sql.ErrNoRows
		}
		var hidden bool
		err := tx.QueryRowContext(ctx, `SELECT hidden FROM mcp_bindings WHERE agent_id=? AND name=?`, owner, name).Scan(&hidden)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if hidden {
			return ErrMCPHidden
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_servers(name,data) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET data=excluded.data`, name, string(data)); err != nil {
		return err
	}
	if owner != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_bindings(agent_id,name,enabled,hidden) VALUES(?,?,1,0) ON CONFLICT(agent_id,name) DO UPDATE SET enabled=1`, owner, name); err != nil {
			return err
		}
		for _, field := range []struct {
			data  json.RawMessage
			query string
		}{
			{local, `UPDATE agent_definitions SET data=json_set(data,'$.mcp',json(?)) WHERE agent_id=?`},
			{devices, `UPDATE agent_definitions SET data=json_set(data,'$.devices',json(?)) WHERE agent_id=?`},
		} {
			if field.data != nil {
				if _, err := tx.ExecContext(ctx, field.query, string(field.data), owner); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

func (s *Store) BindMCP(ctx context.Context, id, name string, enabled, hidden *bool, legacy bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM mcp_servers WHERE name=?`, name).Scan(&count); err != nil {
		return err
	}
	if count == 0 && !legacy {
		return ErrMCPUnknown
	}
	value := MCPBinding{Name: name, Enabled: legacy}
	err = tx.QueryRowContext(ctx, `SELECT enabled,hidden FROM mcp_bindings WHERE agent_id=? AND name=?`, id, name).Scan(&value.Enabled, &value.Hidden)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if hidden != nil {
		value.Hidden = *hidden
		if value.Hidden {
			value.Enabled = false
		}
	}
	if enabled != nil {
		if *enabled && value.Hidden {
			return ErrMCPHidden
		}
		value.Enabled = *enabled
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_bindings(agent_id,name,enabled,hidden) VALUES(?,?,?,?) ON CONFLICT(agent_id,name) DO UPDATE SET enabled=excluded.enabled,hidden=excluded.hidden`, id, name, value.Enabled, value.Hidden); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RemoveIntegrations(ctx context.Context, id, name string, mcp, devices, skills json.RawMessage) error {
	if !json.Valid(mcp) || !json.Valid(devices) || !json.Valid(skills) {
		return ErrMCPUnknown
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var known bool
	if name != "" {
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mcp_servers WHERE name=?)
 OR EXISTS(SELECT 1 FROM mcp_bindings WHERE agent_id=? AND name=?)
 OR EXISTS(SELECT 1 FROM agent_definitions,json_each(agent_definitions.data,'$.mcp') AS item WHERE agent_id=? AND json_extract(item.value,'$.name')=?)`, name, id, name, id, name).Scan(&known)
		if err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_definitions SET data=json_set(data,'$.mcp',json(?),'$.devices',json(?),'$.skills',json(?)) WHERE agent_id=?`, string(mcp), string(devices), string(skills), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	if known {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_bindings(agent_id,name,enabled,hidden) VALUES(?,?,0,0) ON CONFLICT(agent_id,name) DO UPDATE SET enabled=0`, id, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}
