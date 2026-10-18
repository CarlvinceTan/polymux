package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func (s *Store) PublishAttachment(ctx context.Context, attachment contract.Attachment, path string, event contract.Event, message *contract.Message) (contract.Event, error) {
	data, err := json.Marshal(attachment)
	if err != nil {
		return event, fmt.Errorf("encode attachment: %w", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return event, fmt.Errorf("begin attachment publication: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO attachments(id,data,path) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, attachment.ID, string(data), path); err != nil {
		return event, fmt.Errorf("save attachment: %w", err)
	}
	event, err = appendTx(ctx, tx, event, message)
	if err != nil {
		return event, fmt.Errorf("publish attachment: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return event, fmt.Errorf("commit attachment publication: %w", err)
	}
	return event, nil
}
