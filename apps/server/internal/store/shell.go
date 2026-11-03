package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/untrusted"
)

// HasSteers reports a message waiting for this run's next model request.
func (s *Store) HasSteers(ctx context.Context, run string) (bool, error) {
	var waiting bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE state='steer' AND json_extract(input,'$.parentRunID')=?)`, run).Scan(&waiting)
	return waiting, err
}

// QueueShell starts a chat turn with the result of a command that left its run.
func (s *Store) QueueShell(ctx context.Context, parent contract.Run, call, command, output string, failed bool) error {
	outcome := "finished"
	if failed {
		outcome = "failed or was stopped"
	}
	text := "A command or tool call you left running in the background " + outcome + ".\nRan: " + boundedText(command, 2048) +
		"\nOutput:\n" + untrusted.Wrap("command output", output) + "\nUse this to move the task forward."
	return s.queueNote(ctx, parent, "shell:"+parent.ID+":"+call, text)
}

// QueueReload continues a run's chat in a fresh turn, where its new setup is loaded.
func (s *Store) QueueReload(ctx context.Context, parent contract.Run, text string) error {
	return s.queueNote(ctx, parent, "reload:"+parent.ID, text)
}

// RunningRun finds the turn an agent is taking in its own chat.
func (s *Store) RunningRun(ctx context.Context, agent string) (contract.Run, bool, error) {
	var data string
	err := s.DB.QueryRowContext(ctx, `SELECT data FROM runs WHERE thread_id=? AND agent_id=? AND status='running' ORDER BY rowid DESC LIMIT 1`, agent, agent).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return contract.Run{}, false, nil
	}
	if err != nil {
		return contract.Run{}, false, err
	}
	var run contract.Run
	return run, true, json.Unmarshal([]byte(data), &run)
}

func (s *Store) queueNote(ctx context.Context, parent contract.Run, id, text string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=?)`, id).Scan(&exists); err != nil || exists {
		return err
	}
	work := Work{Trigger: contract.RunTriggerFollowUp, ParentRunID: &parent.ID, Message: contract.Message{
		ID: id, ThreadID: parent.ConversationID, Role: contract.MessageRoleSystem, CreatedAt: Now(),
		Parts: []contract.Part{{Kind: contract.PartKindText, Text: &text}},
	}}
	if err := s.queueTx(ctx, tx, work, parent.AgentID); err != nil {
		return err
	}
	return tx.Commit()
}
