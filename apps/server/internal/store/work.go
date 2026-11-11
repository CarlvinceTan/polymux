package store

import (
	"context"
	"encoding/json"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// RecentWork reads the newest runs and deliveries across every chat. Tool output stays out,
// because a view of who is doing what doesn't need it and it can be large.
func (s *Store) RecentWork(ctx context.Context, limit int) (contract.WorkActivity, error) {
	activity := contract.WorkActivity{Runs: []contract.Run{}, Deliveries: []contract.PeerDelivery{}}
	if err := s.newest(ctx, `SELECT data FROM runs ORDER BY rowid DESC LIMIT ?`, limit, func(data []byte) error {
		var run contract.Run
		if err := json.Unmarshal(data, &run); err != nil {
			return err
		}
		for index := range run.Tools {
			run.Tools[index].Output = nil
		}
		activity.Runs = append(activity.Runs, run)
		return nil
	}); err != nil {
		return activity, err
	}
	err := s.newest(ctx, `SELECT data FROM deliveries ORDER BY rowid DESC LIMIT ?`, limit, func(data []byte) error {
		var delivery contract.PeerDelivery
		if err := json.Unmarshal(data, &delivery); err != nil {
			return err
		}
		activity.Deliveries = append(activity.Deliveries, delivery)
		return nil
	})
	return activity, err
}

func (s *Store) newest(ctx context.Context, query string, limit int, read func([]byte) error) error {
	rows, err := s.DB.QueryContext(ctx, query, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return err
		}
		if err := read(data); err != nil {
			return err
		}
	}
	return rows.Err()
}
