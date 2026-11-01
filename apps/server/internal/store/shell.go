package store

import (
	"context"

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
	text := "A command you left running in the background " + outcome + ".\nCommand: " + boundedText(command, 2048) +
		"\nOutput:\n" + untrusted.Wrap("command output", output) + "\nUse this to move the task forward."
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	work := Work{Trigger: contract.RunTriggerFollowUp, ParentRunID: &parent.ID, Message: contract.Message{
		ID: "shell:" + parent.ID + ":" + call, ThreadID: parent.ConversationID, Role: contract.MessageRoleSystem, CreatedAt: Now(),
		Parts: []contract.Part{{Kind: contract.PartKindText, Text: &text}},
	}}
	if err := s.queueTx(ctx, tx, work, parent.AgentID); err != nil {
		return err
	}
	return tx.Commit()
}
