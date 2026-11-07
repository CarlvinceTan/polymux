package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/MichaelKinsy/PiG/durable"
	"github.com/MichaelKinsy/PiG/durable/storage/sqlite"
	"github.com/MichaelKinsy/PiG/durable/storage/sqlite/node"
)

// NewDurableStorage opens durable.db with PiG's native SQLite adapter and history cache.
// The returned closer also handles failed runtime construction; repeated closes are safe.
func NewDurableStorage(dir string) (durable.Storage, func() error, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, err
	}
	database, err := node.OpenNodeSqliteDatabase(filepath.Join(dir, "durable.db"), node.NodeSqliteDatabaseOptions{})
	if err != nil {
		return nil, nil, err
	}
	// Preserve the existing store's power-loss durability instead of the adapter's NORMAL default.
	if err := database.Exec("PRAGMA synchronous = FULL"); err != nil {
		return nil, nil, errors.Join(err, database.Close())
	}
	storage, err := sqlite.Open(database)
	if err != nil {
		return nil, nil, err
	}
	return storage, func() error { return storage.Close(context.Background()) }, nil
}

// Transcripts lists the path of every conversation no other conversation owns.
func (r *DurableRuntime) Transcripts(ctx context.Context) ([]string, error) {
	file := filepath.Join(r.dir, "durable.db")
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		return nil, nil // In-memory storage keeps nothing to list.
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	connection, err := node.NewDatabaseSync(file, node.DatabaseSyncOptions{TimeoutMs: 5000})
	if err != nil {
		return nil, err
	}
	db := node.NewNodeSqliteDatabase(connection)
	defer db.Close()
	rows, err := db.All(`SELECT id FROM conversations WHERE owner_conversation_id IS NULL AND owner_task_id IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		id, ok := row["id"].(int64)
		if !ok {
			return nil, errors.New("conversation id is not an integer")
		}
		paths = append(paths, durablePath(durable.ConversationId(id)))
	}
	return paths, nil
}

// Forget deletes a closed conversation's durable records, with the conversations it owns.
// PiG has no deletion API, so this edits its tables directly; the conversation must not be in use.
func (r *DurableRuntime) Forget(ctx context.Context, path string) error {
	id, ok := parseDurablePath(path)
	if !ok {
		return nil
	}
	file := filepath.Join(r.dir, "durable.db")
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		return nil // In-memory storage keeps nothing to forget.
	}
	// A task that outlives its rows fails its next commit, and with it every conversation.
	if conversation, err := r.harness.Conversation(ctx, id); err == nil && conversation != nil {
		_ = conversation.Abort(ctx, &durable.ConversationAbortOptions{Background: true})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	connection, err := node.NewDatabaseSync(file, node.DatabaseSyncOptions{TimeoutMs: 5000})
	if err != nil {
		return err
	}
	db := node.NewNodeSqliteDatabase(connection)
	defer db.Close()
	return db.Transaction(func(tx sqlite.SqliteExecutor) error {
		// forgotten holds the conversation, every conversation it or its tasks own, and their tasks.
		if err := tx.Exec(`CREATE TEMP TABLE forgotten(id INTEGER PRIMARY KEY, kind TEXT NOT NULL)`); err != nil {
			return err
		}
		for _, statement := range []string{
			`WITH RECURSIVE owned(id) AS (SELECT ? UNION SELECT c.id FROM conversations c, owned o
WHERE c.owner_conversation_id=o.id OR c.owner_task_id IN (SELECT t.id FROM tasks t WHERE t.conversation_id=o.id))
INSERT INTO forgotten SELECT id,'conversation' FROM owned`,
			`INSERT INTO forgotten SELECT id,'task' FROM tasks WHERE conversation_id IN (SELECT id FROM forgotten)`,
			`DELETE FROM document_revisions WHERE document_id IN (SELECT d.id FROM documents d JOIN forgotten f ON f.id=d.owner_id AND f.kind=d.scope_kind)`,
			`DELETE FROM record_ids WHERE id IN (SELECT d.id FROM documents d JOIN forgotten f ON f.id=d.owner_id AND f.kind=d.scope_kind)
OR id IN (SELECT id FROM entries WHERE conversation_id IN (SELECT id FROM forgotten))
OR id IN (SELECT id FROM submissions WHERE conversation_id IN (SELECT id FROM forgotten))
OR id IN (SELECT id FROM forgotten)`,
			`DELETE FROM documents WHERE id IN (SELECT d.id FROM documents d JOIN forgotten f ON f.id=d.owner_id AND f.kind=d.scope_kind)`,
			`DELETE FROM entries WHERE conversation_id IN (SELECT id FROM forgotten)`,
			`DELETE FROM submissions WHERE conversation_id IN (SELECT id FROM forgotten)`,
			`DELETE FROM tasks WHERE id IN (SELECT id FROM forgotten WHERE kind='task')`,
			`DELETE FROM conversations WHERE id IN (SELECT id FROM forgotten WHERE kind='conversation')`,
			`DROP TABLE forgotten`,
		} {
			if err := ctx.Err(); err != nil {
				return err
			}
			args := []any{}
			if strings.Contains(statement, "?") {
				args = append(args, int64(id))
			}
			if err := tx.Run(statement, args...); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}
