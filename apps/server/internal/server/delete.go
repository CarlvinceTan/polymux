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
	s.actorsMu.Unlock()
	return err
}

func (s *Server) removeAgent(ctx context.Context, id string, a *actor, groups ...*groupActor) (bool, error) {
	s.stopShells(id)
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
	// The durable transcripts outlive the store's session rows, so note them first.
	transcripts := []string{}
	if path, err := s.Store.Session(ctx, id); err == nil {
		transcripts = append(transcripts, path)
	}
	for _, group := range groups {
		if path, err := s.Store.GroupSession(ctx, group.id, id); err == nil {
			transcripts = append(transcripts, path)
		}
	}
	files, existed, err := s.Store.DeleteAgent(ctx, id)
	if err != nil {
		return false, err
	}
	cleanupErr := errors.Join(s.removeDraft(id), s.memory.Delete(ctx, id), s.secrets.Delete(id, ""), s.removeMedia(id), s.forget(ctx, transcripts...))
	for _, group := range groups {
		if exists, err := s.Store.HasThread(ctx, group.id); err == nil && !exists {
			cleanupErr = errors.Join(cleanupErr, s.removeDraft(group.id))
		}
	}
	s.removeAttachments(files)
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
	return true, cleanupErr
}

// removeAttachments deletes attachment files and thumbnails inside the attachments folder.
func (s *Server) removeAttachments(files []string) {
	base, err := filepath.Abs(filepath.Join(s.config.DataDir, "attachments"))
	if err != nil {
		return
	}
	for _, path := range files {
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
	transcripts := []string{}
	for _, agent := range thread.AgentIDs {
		if path, err := s.Store.GroupSession(ctx, id, agent); err == nil {
			transcripts = append(transcripts, path)
		}
	}
	files, err := s.Store.DeleteGroup(ctx, id)
	if err != nil {
		return err
	}
	if err := s.removeDraft(id); err != nil {
		return err
	}
	if err := s.forget(ctx, transcripts...); err != nil {
		s.config.Log.Warn("forget deleted group transcripts", "error", err)
	}
	s.removeAttachments(files)
	if !strings.ContainsAny(id, "/\\.") {
		if err := os.RemoveAll(filepath.Join(s.config.DataDir, "groups", id)); err != nil {
			s.config.Log.Warn("remove deleted group sessions", "error", err)
		}
	}
	for agent, workspace := range workspaces {
		if err := s.worktrees.Remove(ctx, workspace, agent, id); err != nil {
			s.config.Log.Warn("remove group worktree", "thread", id, "error", err)
		}
	}
	return nil
}

// forget deletes closed conversations' durable transcripts when the runtime keeps them.
func (s *Server) forget(ctx context.Context, paths ...string) error {
	runtime, ok := s.config.Factory.(interface {
		Forget(context.Context, string) error
	})
	if !ok {
		return nil
	}
	var err error
	for _, path := range paths {
		err = errors.Join(err, runtime.Forget(ctx, path))
	}
	return err
}
