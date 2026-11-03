package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

const (
	sweepWait  = 5 * time.Second
	sweepRetry = 30 * time.Second
)

type deletion struct {
	done chan struct{}
	err  error
}

func (s *Server) deleteThread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	thread, err := s.Store.Thread(r.Context(), id)
	if s.profileError(w, err) {
		return
	}
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	if thread.ID == thread.AgentID {
		fail(w, http.StatusConflict, "not_group", "Delete this bot from its profile instead.")
		return
	}
	err = s.eraseGroup(r.Context(), id)
	if s.profileError(w, err) {
		return
	}
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	write(w, http.StatusOK, contract.DeleteResponse{Deleted: true})
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	err := s.eraseAgent(r.Context(), r.PathValue("id"))
	if s.profileError(w, err) {
		return
	}
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	write(w, 200, contract.DeleteResponse{Deleted: true})
}

func (s *Server) eraseAgent(ctx context.Context, id string) error {
	s.actorsMu.Lock()
	if s.closing {
		s.actorsMu.Unlock()
		return errClosing
	}
	if pending := s.deletes[id]; pending != nil {
		s.actorsMu.Unlock()
		<-pending.done
		return pending.err
	}
	pending := &deletion{done: make(chan struct{})}
	s.deletes[id] = pending
	s.workers.Add(1)
	s.actorsMu.Unlock()
	defer s.workers.Done()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	err := s.eraseReady(ctx, id)
	s.actorsMu.Lock()
	pending.err = err
	delete(s.deletes, id)
	close(pending.done)
	s.actorsMu.Unlock()
	return err
}

func (s *Server) eraseReady(ctx context.Context, id string) error {
	s.actorsMu.Lock()
	if s.closing {
		s.actorsMu.Unlock()
		return errClosing
	}
	threads, err := s.Store.Threads(ctx)
	if err != nil {
		s.actorsMu.Unlock()
		return err
	}
	var groups []*groupActor
	for _, thread := range threads {
		if group := s.groups[thread.ID]; group != nil && slices.Contains(thread.AgentIDs, id) {
			group.halt()
			groups = append(groups, group)
			delete(s.groups, thread.ID)
		}
	}
	a := s.actors[id]
	delete(s.actors, id)
	s.actorsMu.Unlock()
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
	if err == nil {
		err = s.removeAgent(ctx, id, a, groups...)
	}
	s.actorsMu.Lock()
	if !s.closing && err == nil {
		for _, group := range groups {
			if thread, loadErr := s.Store.Thread(ctx, group.id); loadErr == nil {
				s.startGroup(thread.ID)
			}
		}
	}
	s.actorsMu.Unlock()
	return err
}

func (s *Server) removeAgent(ctx context.Context, id string, a *actor, groups ...*groupActor) error {
	s.stopShells(id)
	if err := s.stopVoices(ctx, id); err != nil {
		return err
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
		return err
	}
	for _, runID := range peers {
		if active, ok := s.turns.Load(runID); ok {
			turn := active.(*execution)
			turn.cancel()
			select {
			case <-turn.done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if err := s.Store.DeleteAgent(ctx, id); err != nil {
		return err
	}
	s.settle(ctx)
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
		entries, err := os.ReadDir(filepath.Join(s.config.DataDir, "worktrees"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			s.config.Log.Warn("read task worktrees", "error", err)
		}
		for _, entry := range entries {
			if entry.IsDir() && (strings.HasPrefix(entry.Name(), id+"--child-") || strings.HasPrefix(entry.Name(), id+"--peer-")) {
				threads = append(threads, strings.TrimPrefix(entry.Name(), id+"--"))
			}
		}
		for _, thread := range threads {
			if err := s.worktrees.Remove(ctx, workspace, id, thread); err != nil {
				s.config.Log.Warn("remove conversation worktree", "thread", thread, "error", err)
			}
		}
	}
	return nil
}

// removeAttachment deletes an attachment file and thumbnail inside the attachments folder.
func (s *Server) removeAttachment(path string) error {
	base, err := filepath.Abs(filepath.Join(s.config.DataDir, "attachments"))
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(base, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil
	}
	var problems []error
	for _, file := range []string{path, path + ".jpg"} {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// removeFolder deletes a folder's sessions. Files the agent wrote stay; only a folder left empty goes.
func removeFolder(dir string) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if err := os.RemoveAll(filepath.Join(dir, "sessions")); err != nil {
		return err
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, syscall.ENOTEMPTY) {
		return err
	}
	return nil
}

// eraseGroup stops a group chat and deletes it with its sessions and checkouts. Members stay.
func (s *Server) eraseGroup(ctx context.Context, id string) error {
	thread, err := s.Store.Thread(ctx, id)
	if err != nil {
		return err
	}
	s.actorsMu.Lock()
	if s.closing {
		s.actorsMu.Unlock()
		return errClosing
	}
	group := s.groups[id]
	delete(s.groups, id)
	workspaces := map[string]string{}
	for _, agent := range thread.AgentIDs {
		if a := s.actors[agent]; a != nil {
			a.mu.Lock()
			if a.definition.Workspace != "" {
				workspaces[agent] = a.definition.Workspace
			}
			a.mu.Unlock()
		}
	}
	s.actorsMu.Unlock()
	if group != nil {
		group.halt()
		group.stop()
		select {
		case <-group.done:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := group.close(); err != nil {
			s.config.Log.Warn("close deleted group", "error", err)
		}
	}
	if err := s.Store.DeleteGroup(ctx, id); err != nil {
		return err
	}
	s.settle(ctx)
	for agent, workspace := range workspaces {
		if err := s.worktrees.Remove(ctx, workspace, agent, id); err != nil {
			s.config.Log.Warn("remove group worktree", "thread", id, "error", err)
		}
	}
	return nil
}

// settle tries the cleanups a deletion just committed and leaves any that fail to the sweeper.
func (s *Server) settle(ctx context.Context) {
	if s.sweep(ctx) {
		s.wakeSweep()
	}
}

// discard queues a finished turn's transcript: nothing saved its path, so nothing can reopen it.
func (s *Server) discard(ctx context.Context, session agentruntime.Session) {
	path := session.Path()
	if durable, ok := session.(interface{ Transcript() string }); ok {
		path = durable.Transcript()
	}
	if path == "" {
		return
	}
	if err := s.Store.AddCleanup(ctx, store.Cleanup{Kind: store.CleanupTranscript, Target: path}); err != nil {
		s.config.Log.Warn("queue finished transcript", "error", err)
	}
	s.wakeSweep()
}

// orphans lists conversations nothing can reopen, such as those of a crashed turn. It must run before any turn starts,
// while every live conversation has a saved path.
func (s *Server) orphans(ctx context.Context) []string {
	runtime, ok := s.config.Factory.(interface {
		Transcripts(context.Context) ([]string, error)
	})
	if !ok {
		return nil
	}
	saved, err := s.Store.SavedSessions(ctx)
	if err != nil {
		s.config.Log.Warn("list saved sessions", "error", err)
		return nil
	}
	all, err := runtime.Transcripts(ctx)
	if err != nil {
		s.config.Log.Warn("list transcripts", "error", err)
		return nil
	}
	// A store that names none of them may be new or restored, so it proves nothing.
	orphaned := slices.DeleteFunc(slices.Clone(all), func(path string) bool { return slices.Contains(saved, path) })
	if len(orphaned) == len(all) {
		return nil
	}
	return orphaned
}

func (s *Server) wakeSweep() {
	select {
	case s.sweepWake <- struct{}{}:
	default:
	}
}

// runSweeps forgets orphans once, then finishes cleanups that failed or were cut short by a restart.
func (s *Server) runSweeps(ctx context.Context, orphans []string) {
	defer s.workers.Done()
	for _, path := range orphans {
		step := store.Cleanup{Kind: store.CleanupTranscript, Target: path}
		attempt, cancel := context.WithTimeout(ctx, sweepWait)
		err := s.clean(attempt, step)
		cancel()
		if ctx.Err() != nil {
			return // The next start lists it again.
		}
		if err != nil {
			s.config.Log.Warn("forget orphaned transcript", "path", path, "error", err)
			if err := s.Store.AddCleanup(ctx, step); err != nil {
				s.config.Log.Warn("queue orphaned transcript", "error", err)
			}
		}
	}
	for {
		var retry <-chan time.Time
		if s.sweep(ctx) {
			retry = time.After(sweepRetry)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.sweepWake:
		case <-retry:
		}
	}
}

// sweep runs every pending cleanup once and reports whether any remain.
func (s *Server) sweep(ctx context.Context) bool {
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	steps, err := s.Store.Cleanups(ctx)
	if err != nil {
		s.config.Log.Warn("list cleanups", "error", err)
		return true
	}
	left := false
	for _, step := range steps {
		attempt, cancel := context.WithTimeout(ctx, sweepWait)
		err := s.clean(attempt, step)
		if err == nil {
			err = s.Store.Cleaned(attempt, step)
		}
		cancel()
		if err != nil {
			s.config.Log.Warn("deletion cleanup", "kind", step.Kind, "target", step.Target, "error", err)
			left = true
		}
	}
	return left
}

func (s *Server) clean(ctx context.Context, step store.Cleanup) error {
	switch step.Kind {
	case store.CleanupAgent:
		id := step.Target
		return errors.Join(s.removeDraft(id), s.memory.Delete(ctx, id), s.secrets.Delete(id, ""), s.removeMedia(id), removeFolder(agentruntime.AgentDir(s.config.DataDir, id)))
	case store.CleanupAttachment:
		return s.removeAttachment(step.Target)
	case store.CleanupDraft:
		return s.removeDraft(step.Target)
	case store.CleanupGroup:
		if strings.ContainsAny(step.Target, "/\\.") {
			return nil
		}
		return errors.Join(s.removeDraft(step.Target), os.RemoveAll(filepath.Join(s.config.DataDir, "groups", step.Target)))
	case store.CleanupMember:
		if !filepath.IsLocal(step.Target) {
			return nil
		}
		return removeFolder(filepath.Join(s.config.DataDir, "groups", step.Target))
	case store.CleanupTranscript:
		if runtime, ok := s.config.Factory.(interface {
			Forget(context.Context, string) error
		}); ok {
			return runtime.Forget(ctx, step.Target)
		}
	}
	return nil
}
