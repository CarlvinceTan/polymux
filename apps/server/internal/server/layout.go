package server

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

// moveAgents moves agent folders from the top of the data directory into agents/ and drops
// the empty ones of deleted agents. Each rename is atomic, so a run cut short carries on
// from whatever is still at the top.
func moveAgents(ctx context.Context, s *store.Store, dir string, definitions []agentruntime.Definition, log *slog.Logger) error {
	live := make(map[string]bool, len(definitions))
	for _, d := range definitions {
		live[d.Agent.ID] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	moved := 0
	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() || id == "agents" {
			continue
		}
		deleted, err := s.Deleted(ctx, id)
		if err != nil {
			return err
		}
		if !live[id] && !deleted {
			continue
		}
		old, next := filepath.Join(dir, id), agentruntime.AgentDir(dir, id)
		_, err = os.Lstat(next)
		if !live[id] || err == nil {
			// Only an empty folder goes; one that holds files carries on below.
			err := os.Remove(old)
			if err == nil {
				continue
			}
			if !errors.Is(err, syscall.ENOTEMPTY) {
				return err
			}
		}
		// Rename refuses an existing folder, so clear an empty one first.
		if err := os.Remove(next); errors.Is(err, syscall.ENOTEMPTY) {
			log.Warn("agent folder exists in both layouts; keeping both", "agent", id)
			continue
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(next), 0700); err != nil {
			return err
		}
		if err := os.Rename(old, next); err != nil {
			return err
		}
		moved++
	}
	if moved > 0 {
		log.Info("moved agent folders", "count", moved)
	}
	return nil
}
