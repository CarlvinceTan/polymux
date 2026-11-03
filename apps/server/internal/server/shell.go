package server

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// shellJob is a command that left its turn so the agent could answer a message.
type shellJob struct {
	thread, agent string
	stop          context.CancelFunc
	quiet         atomic.Bool
}

func (s *Server) shellHost(run contract.Run) *agentruntime.ShellHost {
	return &agentruntime.ShellHost{
		Waiting: func(ctx context.Context) bool {
			waiting, err := s.Store.HasSteers(ctx, run.ID)
			if err != nil && ctx.Err() == nil {
				s.config.Log.Error("check waiting messages", "run", run.ID, "error", err)
			}
			return waiting
		},
		Detach: func(id string, stop context.CancelFunc) func(agentruntime.ShellResult) {
			s.actorsMu.RLock()
			defer s.actorsMu.RUnlock()
			if s.closing {
				return nil
			}
			job := &shellJob{thread: run.ConversationID, agent: run.AgentID, stop: stop}
			key := run.ID + ":" + id
			s.shells.Store(key, job)
			s.workers.Add(1)
			// An update waits for the command, as it does for the turn that started it.
			s.admission.fork()
			return func(result agentruntime.ShellResult) {
				defer s.workers.Done()
				defer s.admission.done()
				s.shells.Delete(key)
				if job.quiet.Load() {
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := s.Store.QueueShell(ctx, run, result.ID, result.Command, result.Output, result.Failed); err != nil {
					s.config.Log.Error("report background command", "run", run.ID, "error", err)
				}
			}
		},
	}
}

// stopShells ends what a thread or agent left running, because the user asked for that.
func (s *Server) stopShells(id string) {
	s.shells.Range(func(_, value any) bool {
		if job := value.(*shellJob); job.thread == id || job.agent == id {
			job.quiet.Store(true)
			job.stop()
		}
		return true
	})
}
