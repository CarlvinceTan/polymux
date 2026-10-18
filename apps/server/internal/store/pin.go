package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
)

var ErrPinLimit = errors.New("pin limit reached")
var ErrPinOrder = errors.New("invalid pin order")

func (s *Store) migratePins(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, column := range []struct{ name, statement string }{
		{"pinned", `ALTER TABLE threads ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`},
		{"pin_order", `ALTER TABLE threads ADD COLUMN pin_order REAL`},
	} {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('threads') WHERE name=?`, column.name).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := tx.ExecContext(ctx, column.statement); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func updatePin(ctx context.Context, tx *sql.Tx, id string, pinned *bool, order *float64) error {
	if pinned == nil && order == nil {
		return nil
	}
	var current bool
	var position *float64
	if err := tx.QueryRowContext(ctx, `SELECT pinned,pin_order FROM threads WHERE id=?`, id).Scan(&current, &position); err != nil {
		return err
	}
	next := current
	if pinned != nil {
		next = *pinned
	}
	if order != nil && (!next || math.IsNaN(*order) || math.IsInf(*order, 0) || *order < 0) {
		return ErrPinOrder
	}
	if next && !current {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM threads WHERE pinned=1`).Scan(&count); err != nil {
			return err
		}
		if count >= 12 {
			return ErrPinLimit
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(pin_order)+1,0) FROM threads WHERE pinned=1`).Scan(&position); err != nil {
			return err
		}
	}
	if order != nil {
		position = order
	}
	if !next {
		position = nil
	}
	if next {
		if _, err := tx.ExecContext(ctx, `UPDATE threads SET section_id=NULL,section_order=NULL WHERE id=?`, id); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE threads SET pinned=?,pin_order=? WHERE id=?`, next, position, id)
	return err
}
