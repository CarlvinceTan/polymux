package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

type deletion struct {
	done chan struct{}
	err  error
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.actorsMu.Lock()
	if s.closing {
		s.actorsMu.Unlock()
		fail(w, 503, "closing", "The server is restarting. Try again shortly.")
		return
	}
	if pending := s.deletes[id]; pending != nil {
		s.actorsMu.Unlock()
		<-pending.done
		if pending.err != nil {
			s.respond(w, nil, pending.err)
		} else {
			write(w, 200, contract.DeleteResponse{Deleted: true})
		}
		return
	}
	threads, err := s.Store.Threads(r.Context())
	if err != nil {
		s.actorsMu.Unlock()
		s.respond(w, nil, err)
		return
	}
	var groups []*groupActor
	for _, thread := range threads {
		if group := s.groups[thread.ID]; group != nil && slices.Contains(thread.AgentIDs, id) {
			group.mu.Lock()
			group.stopping = true
			if group.cancel != nil {
				group.cancel()
			}
			group.mu.Unlock()
			groups = append(groups, group)
			delete(s.groups, thread.ID)
		}
	}
	pending := &deletion{done: make(chan struct{})}
	s.deletes[id] = pending
	a := s.actors[id]
	delete(s.actors, id)
	s.workers.Add(1)
	s.actorsMu.Unlock()
	defer s.workers.Done()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	for _, group := range groups {
		select {
		case <-group.done:
			group.stop()
			group.close()
		case <-ctx.Done():
			err = ctx.Err()
		}
		if err != nil {
			break
		}
	}
	removed := false
	if err == nil {
		removed, err = s.removeAgent(ctx, id, a, groups...)
	}
	s.actorsMu.Lock()
	if !s.closing && removed {
		for _, group := range groups {
			if thread, loadErr := s.Store.Thread(ctx, group.id); loadErr == nil {
				s.startGroup(thread.ID)
			}
		}
	}
	pending.err = err
	delete(s.deletes, id)
	close(pending.done)
	s.actorsMu.Unlock()
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	write(w, 200, contract.DeleteResponse{Deleted: true})
}

func (s *Server) removeAgent(ctx context.Context, id string, a *actor, groups ...*groupActor) (bool, error) {
	if err := s.stopVoices(ctx, id); err != nil {
		return false, err
	}
	if a != nil {
		a.stop()
		a.sessionMu.Lock()
		a.mu.Lock()
		session := a.session
		cancel := a.cancel
		a.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if session != nil {
			abortCtx, end := context.WithTimeout(ctx, 5*time.Second)
			if err := session.Abort(abortCtx); err != nil && !errors.Is(err, context.Canceled) {
				s.config.Log.Warn("abort deleted agent", "error", err)
			}
			end()
		}
		a.sessionMu.Unlock()
		<-a.done
		a.sessionMu.Lock()
		a.mu.Lock()
		if a.session != nil {
			if err := a.session.Close(); err != nil {
				s.config.Log.Warn("close deleted agent", "error", err)
			}
			a.session = nil
		}
		a.mu.Unlock()
		a.sessionMu.Unlock()
	}
	peers, err := s.Store.PendingPeers(ctx, id)
	if err != nil {
		return false, err
	}
	for _, runID := range peers {
		if active, ok := s.turns.Load(runID); ok {
			turn := active.(*execution)
			turn.cancel()
			select {
			case <-turn.done:
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}
	}
	files, existed, err := s.Store.DeleteAgent(ctx, id)
	if err != nil {
		return false, err
	}
	cleanupErr := s.memory.Delete(ctx, id)
	for _, path := range files {
		base, err := filepath.Abs(filepath.Join(s.config.DataDir, "attachments"))
		if err != nil {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(base, absolute)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		for _, file := range []string{path, path + ".jpg"} {
			if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.config.Log.Warn("remove deleted attachment", "error", err)
			}
		}
	}
	if existed && id != "" && !strings.ContainsAny(id, "/\\.") {
		dirs := []string{filepath.Join(s.config.DataDir, id)}
		for _, group := range groups {
			dirs = append(dirs, filepath.Join(s.config.DataDir, "groups", group.id, id))
		}
		for _, dir := range dirs {
			if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink == 0 {
				if err := os.RemoveAll(filepath.Join(dir, "sessions")); err != nil {
					s.config.Log.Warn("remove deleted session", "error", err)
				}
			}
		}
	}
	var workspace string
	if a != nil {
		a.mu.Lock()
		workspace = a.definition.Workspace
		a.mu.Unlock()
	}
	if workspace != "" {
		// The agent's own chat, plus any group thread this deletion emptied. A
		// dirty checkout is left in place; the warning names the reason.
		threads := []string{id}
		for _, group := range groups {
			if _, err := s.Store.Thread(ctx, group.id); err == nil {
				continue
			}
			threads = append(threads, group.id)
		}
		for _, thread := range threads {
			if err := s.worktrees.Remove(ctx, workspace, id, thread); err != nil {
				s.config.Log.Warn("remove conversation worktree", "thread", thread, "error", err)
			}
		}
	}
	return true, cleanupErr
}
