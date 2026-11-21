package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// A merge brings another server's data into this one. Every table has one of three fates, and
// TestEveryTableHasAMergeRule fails on one that has none, so a new table forces a decision.

// mergeCopied are copied row for row, in this order so parents arrive first. Their IDs are
// random, so a clash means something's wrong: the insert fails and nothing is overwritten.
var mergeCopied = []string{
	"agents", "agent_definitions", "threads", "thread_members", "messages", "events", "attachments",
	"media_index", "media_vectors", "search_entities", "jobs", "runs", "effects", "deliveries",
	"task_threads", "tasks", "open_work", "routines", "routine_failures", "routine_watches",
	"triggers", "trigger_events", "proactivity", "memory_reviews", "mcp_bindings", "project_members",
}

// mergeKept are shared by name, and this server's version wins.
var mergeKept = []string{"deleted_agents", "mcp_servers", "projects"}

// mergeHandled have their own step in Merge.
var mergeHandled = []string{"sections", "messages_fts", "plugin_chats"}

// mergeSkipped stay behind, each for the reason given.
var mergeSkipped = map[string]string{
	"cleanups":        "deletions the other server still owed itself",
	"credentials":     "sealed to the other server's key; the server re-seals them",
	"group_sessions":  "point into durable.db, which doesn't merge",
	"notices":         "alerts already sent, numbered for one server",
	"plugin_layout":   "this server's layout stands",
	"plugins":         "plugins don't merge",
	"push_devices":    "devices register with whichever server they use",
	"push_queue":      "follows notices",
	"run_submissions": "point into durable.db, which doesn't merge",
	"sessions":        "point into durable.db, which doesn't merge",
	"sidebar_audit":   "a log numbered for one server",
	"sidebar_events":  "a log numbered for one server",
	"sidebar_sources": "this server's sidebar sources stand",
	"vault_items":     "sealed to the other server's key; the server re-seals them",
	"vault_meta":      "this server's vault stands",
}

// Merged says what became of another server's data.
type Merged struct {
	Bots  int
	Chats int
}

// Merge imports the database at path in one transaction. name is the other server's, for telling
// its things apart. from and to are its data folder and this one, for the paths stored in rows.
func (s *Store) Merge(ctx context.Context, path, name, from, to string) (Merged, error) {
	var result Merged
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS src`, path); err != nil {
		return result, err
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE src`)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if result, err = mergeTx(ctx, tx, name, from, to); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func mergeTx(ctx context.Context, tx *sql.Tx, name, from, to string) (Merged, error) {
	var result Merged
	sections, err := mergeSections(ctx, tx, name)
	if err != nil {
		return result, err
	}
	for _, table := range mergeCopied {
		if err := copyRows(ctx, tx, table, ""); err != nil {
			return result, fmt.Errorf("merge %s: %w", table, err)
		}
	}
	for _, table := range mergeKept {
		if err := copyRows(ctx, tx, table, "OR IGNORE"); err != nil {
			return result, fmt.Errorf("merge %s: %w", table, err)
		}
	}
	steps := []string{
		`INSERT INTO main.messages_fts(message_id,thread_id,body) SELECT message_id,thread_id,body FROM src.messages_fts`,
		`INSERT INTO main.plugin_chats(name,thread_id) SELECT name,thread_id FROM src.plugin_chats WHERE name IN (SELECT name FROM main.plugins)`,
		// Nothing merged runs on its own until someone has looked at it.
		`UPDATE main.routines SET next_at=NULL,data=json_set(data,'$.status','paused')
			WHERE id IN (SELECT id FROM src.routines) AND json_extract(data,'$.status') IN ('pending','active')`,
		`UPDATE main.triggers SET data=json_set(data,'$.enabled',json('false')) WHERE id IN (SELECT id FROM src.triggers)`,
		`UPDATE main.proactivity SET next_at=NULL,data=json_set(data,'$.enabled',json('false')) WHERE agent_id IN (SELECT agent_id FROM src.proactivity)`,
		`UPDATE main.tasks SET status='parked',next_at=NULL,data=json_set(data,'$.status','parked')
			WHERE id IN (SELECT id FROM src.tasks) AND status='sleeping'`,
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step); err != nil {
			return result, fmt.Errorf("merge: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE main.threads SET section_id=? WHERE id IN (SELECT id FROM src.threads) AND (section_id IS NULL OR section_id=?)`, sections, UnassignedID); err != nil {
		return result, err
	}
	if err := rebase(ctx, tx, from, to); err != nil {
		return result, err
	}
	if err := tagBots(ctx, tx, name); err != nil {
		return result, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM src.agents),(SELECT count(*) FROM src.threads)`).Scan(&result.Bots, &result.Chats); err != nil {
		return result, err
	}
	return result, nil
}

// copyRows names its columns, because the same table can have them in a different order on a
// database that was migrated rather than created.
func copyRows(ctx context.Context, tx *sql.Tx, table, mode string) error {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info(?,'main')`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns := []string{}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return err
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	list := strings.Join(columns, ",")
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`INSERT %s INTO main.%s(%s) SELECT %s FROM src.%s`, mode, table, list, list, table))
	return err
}

// mergeSections brings the other server's sections after this one's, and returns one named after
// it for chats that weren't in any. Names are unique here, so a clash takes on the server's name.
func mergeSections(ctx context.Context, tx *sql.Tx, name string) (string, error) {
	var position float64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(position),0) FROM main.sections`).Scan(&position); err != nil {
		return "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,name,collapsed,created_at,updated_at FROM src.sections WHERE id!=? ORDER BY position`, UnassignedID)
	if err != nil {
		return "", err
	}
	type section struct {
		id, name, created, updated string
		collapsed                  int
	}
	found := []section{}
	for rows.Next() {
		var item section
		if err := rows.Scan(&item.id, &item.name, &item.collapsed, &item.created, &item.updated); err != nil {
			rows.Close()
			return "", err
		}
		found = append(found, item)
	}
	if err := errorsIn(rows); err != nil {
		return "", err
	}
	free := func(wanted string) (string, error) {
		for _, candidate := range []string{wanted, wanted + " (" + name + ")"} {
			var taken int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM main.sections WHERE name=? COLLATE NOCASE`, candidate).Scan(&taken); err != nil {
				return "", err
			}
			if taken == 0 {
				return candidate, nil
			}
		}
		return "", fmt.Errorf("the section name %q is taken", wanted)
	}
	for _, item := range found {
		label, err := free(item.name)
		if err != nil {
			return "", err
		}
		position++
		if _, err := tx.ExecContext(ctx, `INSERT INTO main.sections(id,name,position,collapsed,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
			item.id, label, position, item.collapsed, item.created, item.updated); err != nil {
			return "", err
		}
	}
	label, err := free(name)
	if err != nil {
		return "", err
	}
	var id, now string
	if err := tx.QueryRowContext(ctx, `SELECT 'merged-'||lower(hex(randomblob(12))),strftime('%Y-%m-%dT%H:%M:%SZ','now')`).Scan(&id, &now); err != nil {
		return "", err
	}
	position++
	_, err = tx.ExecContext(ctx, `INSERT INTO main.sections(id,name,position,collapsed,created_at,updated_at) VALUES(?,?,?,0,?,?)`, id, label, position, now, now)
	return id, err
}

func errorsIn(rows *sql.Rows) error {
	defer rows.Close()
	return rows.Err()
}

// tagBots tells apart bots that share a name, since both stay.
func tagBots(ctx context.Context, tx *sql.Tx, name string) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.id,json_extract(s.data,'$.name') FROM src.agents s
		WHERE EXISTS(SELECT 1 FROM main.agents a WHERE a.id NOT IN (SELECT id FROM src.agents)
			AND json_extract(a.data,'$.name')=json_extract(s.data,'$.name') COLLATE NOCASE)`)
	if err != nil {
		return err
	}
	clashes := [][2]string{}
	for rows.Next() {
		var id, bot string
		if err := rows.Scan(&id, &bot); err != nil {
			rows.Close()
			return err
		}
		clashes = append(clashes, [2]string{id, bot + " (" + name + ")"})
	}
	if err := errorsIn(rows); err != nil {
		return err
	}
	for _, clash := range clashes {
		if _, err := tx.ExecContext(ctx, `UPDATE main.agents SET data=json_set(data,'$.name',?) WHERE id=?`, clash[1], clash[0]); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE main.agent_definitions SET data=json_set(data,'$.agent.name',?) WHERE agent_id=?`, clash[1], clash[0]); err != nil {
			return err
		}
	}
	return nil
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// rebase points rows at this server's data folder, for whatever moved here from another.
func rebase(ctx context.Context, db execer, from, to string) error {
	if from == "" || from == to {
		return nil
	}
	_, err := db.ExecContext(ctx, `UPDATE main.attachments SET path=?||substr(path,?) WHERE substr(path,1,?)=?`, to, len(from)+1, len(from), from)
	return err
}

// Rebase points this server's rows at the folder its data now lives in.
func (s *Store) Rebase(ctx context.Context, from, to string) error {
	return rebase(ctx, s.DB, from, to)
}

// MergeRules lists every table with a rule, for the test that keeps it complete.
func MergeRules() []string {
	rules := slices.Concat(mergeCopied, mergeKept, mergeHandled)
	for table := range mergeSkipped {
		rules = append(rules, table)
	}
	return rules
}
