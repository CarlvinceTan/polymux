package agentruntime

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"

	"github.com/MichaelKinsy/PiG/durable"
	"github.com/MichaelKinsy/PiG/durable/storage/sqlite"
	_ "github.com/mattn/go-sqlite3"
)

// NewDurableStorage opens the durable SQLite storage for a Polymux data directory. It is its own
// database, so durable's schema and Polymux's product schema stay independent.
func NewDurableStorage(dir string) (durable.Storage, func() error, error) {
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(dir, "durable.db")+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, nil, err
	}
	storage, err := sqlite.Open(&sqliteDatabase{db: db})
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return storage, db.Close, nil
}

// sqliteDatabase adapts Polymux's *sql.DB to durable's SQLite facade. Durable requires unrelated
// operations and other transactions to queue behind a running transaction, in call order, so
// every operation takes the lock. Polymux's own database keeps its single connection separate.
type sqliteDatabase struct {
	mu sync.Mutex
	db *sql.DB
}

func (d *sqliteDatabase) Exec(text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(text)
	return err
}

func (d *sqliteDatabase) Run(text string, params ...sqlite.SqliteValue) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(text, params...)
	return err
}

func (d *sqliteDatabase) Get(text string, params ...sqlite.SqliteValue) (sqlite.SqliteRow, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.db.Query(text, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all, err := scanRows(rows)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all[0], nil
}

func (d *sqliteDatabase) All(text string, params ...sqlite.SqliteValue) ([]sqlite.SqliteRow, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.db.Query(text, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

// Transaction holds the lock for the whole transaction, so no unrelated statement interleaves.
func (d *sqliteDatabase) Transaction(callback func(transaction sqlite.SqliteExecutor) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	if err := callback(&sqliteTx{tx: tx}); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// Close is a no-op: NewDurableStorage owns the handle and closes it separately.
func (d *sqliteDatabase) Close() error { return nil }

type sqliteTx struct{ tx *sql.Tx }

func (t *sqliteTx) Exec(text string) error {
	_, err := t.tx.Exec(text)
	return err
}

func (t *sqliteTx) Run(text string, params ...sqlite.SqliteValue) error {
	_, err := t.tx.Exec(text, params...)
	return err
}

func (t *sqliteTx) Get(text string, params ...sqlite.SqliteValue) (sqlite.SqliteRow, error) {
	rows, err := t.tx.Query(text, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all, err := scanRows(rows)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all[0], nil
}

func (t *sqliteTx) All(text string, params ...sqlite.SqliteValue) ([]sqlite.SqliteRow, error) {
	rows, err := t.tx.Query(text, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

func scanRows(rows *sql.Rows) ([]sqlite.SqliteRow, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []sqlite.SqliteRow{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := sqlite.SqliteRow{}
		for i, name := range columns {
			row[name] = values[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
