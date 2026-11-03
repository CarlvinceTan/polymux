package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type PluginRecord struct {
	Name  string
	Data  json.RawMessage
	Chats []string
}

func (s *Store) migratePlugins(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS plugins(name TEXT PRIMARY KEY,data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS plugin_chats(name TEXT NOT NULL REFERENCES plugins(name) ON DELETE CASCADE,thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,PRIMARY KEY(name,thread_id));
CREATE INDEX IF NOT EXISTS plugin_chats_thread ON plugin_chats(thread_id);
CREATE TABLE IF NOT EXISTS plugin_layout(id INTEGER PRIMARY KEY CHECK(id=1),data TEXT NOT NULL);`)
	return err
}

func (s *Store) Plugins(ctx context.Context) ([]PluginRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT name,data FROM plugins ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PluginRecord{}
	for rows.Next() {
		var name, data string
		if err := rows.Scan(&name, &data); err != nil {
			return nil, err
		}
		out = append(out, PluginRecord{Name: name, Data: json.RawMessage(data), Chats: []string{}})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	chats, err := s.DB.QueryContext(ctx, `SELECT name,thread_id FROM plugin_chats ORDER BY name,thread_id`)
	if err != nil {
		return nil, err
	}
	defer chats.Close()
	for chats.Next() {
		var name, thread string
		if err := chats.Scan(&name, &thread); err != nil {
			return nil, err
		}
		for index := range out {
			if out[index].Name == name {
				out[index].Chats = append(out[index].Chats, thread)
			}
		}
	}
	return out, chats.Err()
}

func (s *Store) SavePlugin(ctx context.Context, name string, data json.RawMessage) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO plugins(name,data) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET data=excluded.data`, name, string(data))
	return err
}

// RemovePlugin also clears the chats it was active in.
func (s *Store) RemovePlugin(ctx context.Context, name string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM plugins WHERE name=?`, name)
	return err
}

func (s *Store) SetPluginChats(ctx context.Context, name string, chats []string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM plugin_chats WHERE name=?`, name); err != nil {
		return err
	}
	for _, chat := range chats {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO plugin_chats(name,thread_id) VALUES(?,?)`, name, chat); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) PluginLayout(ctx context.Context) (json.RawMessage, error) {
	var data string
	err := s.DB.QueryRowContext(ctx, `SELECT data FROM plugin_layout WHERE id=1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return json.RawMessage(data), err
}

func (s *Store) SavePluginLayout(ctx context.Context, data json.RawMessage) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO plugin_layout(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, string(data))
	return err
}
