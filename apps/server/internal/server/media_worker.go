package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func (s *Server) runMedia(ctx context.Context) {
	defer s.workers.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		s.checkMedia(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) checkMedia(ctx context.Context, now time.Time) {
	dir := filepath.Join(s.config.DataDir, "media-jobs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && ctx.Err() == nil {
			s.config.Log.Error("read media jobs", "error", err)
		}
		return
	}
	var jobs []mediaJob
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		var job mediaJob
		if err != nil || json.Unmarshal(data, &job) != nil || job.ID+".json" != entry.Name() || job.Owner == "" || job.Conversation == "" || job.Message == "" || job.Call == "" {
			continue
		}
		if !job.Delivered && job.NextAt <= now.Unix() && slices.Contains([]string{"pending", "ready", "failed"}, job.Status) {
			jobs = append(jobs, job)
		}
	}
	slices.SortStableFunc(jobs, func(a, b mediaJob) int {
		if a.NextAt < b.NextAt {
			return -1
		}
		if a.NextAt > b.NextAt {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	for _, job := range jobs[:min(16, len(jobs))] {
		if ctx.Err() != nil {
			return
		}
		check, cancel := context.WithTimeout(ctx, 30*time.Second)
		s.completeMedia(check, dir, job.ID, now)
		cancel()
	}
}

func (s *Server) completeMedia(ctx context.Context, dir, id string, now time.Time) {
	s.outputs.jobs.Lock()
	defer s.outputs.jobs.Unlock()
	path := filepath.Join(dir, id+".json")
	data, err := os.ReadFile(path)
	var job mediaJob
	if err != nil || json.Unmarshal(data, &job) != nil || job.Delivered || job.NextAt > now.Unix() {
		return
	}
	member, err := s.Store.Member(ctx, job.Owner, job.Conversation)
	if err != nil || !member {
		return
	}
	tool := &mediaTool{server: s, files: s.outputs, kind: job.Kind}
	if job.Status == "pending" {
		provider, err := tool.jobProvider(job)
		if err == nil {
			headers, keyErr := tool.headers(ctx, job.Owner, provider)
			err = keyErr
			if err == nil {
				err = tool.advance(ctx, dir, &job, headers)
			}
		}
		if err != nil {
			job.NextAt = now.Add(30 * time.Second).Unix()
			if saveErr := saveMediaJob(path, job); saveErr != nil && ctx.Err() == nil {
				s.config.Log.Error("save media retry", "job", id, "error", saveErr)
			}
			return
		}
	}
	if job.Status != "ready" && job.Status != "failed" {
		return
	}
	scope := &htmlScope{messageID: job.Message, publish: func(ctx context.Context, attachment contract.Attachment, path string) error {
		return s.publishMedia(ctx, job, &attachment, path)
	}}
	ctx = context.WithValue(ctx, htmlKey{}, scope)
	if job.Status == "ready" {
		_, err = tool.deliver(ctx, dir, &job)
	} else {
		err = s.publishMedia(ctx, job, nil, "")
		if err == nil {
			job.Delivered = true
			err = saveMediaJob(path, job)
		}
	}
	if err != nil && ctx.Err() == nil {
		s.config.Log.Error("deliver media result", "job", id, "error", err)
	}
}

func (s *Server) publishMedia(ctx context.Context, job mediaJob, attachment *contract.Attachment, path string) error {
	s.actorsMu.RLock()
	defer s.actorsMu.RUnlock()
	if s.closing || s.deletes[job.Owner] != nil {
		return errors.New("media delivery target is unavailable")
	}
	member, err := s.Store.Member(ctx, job.Owner, job.Conversation)
	if err != nil {
		return err
	}
	if !member {
		return store.ErrMembers
	}
	id := "media-result:" + job.ID
	if _, err := s.Store.Message(ctx, id); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	message := contract.Message{ID: id, ThreadID: job.Conversation, AgentID: &job.Owner, Role: "assistant", Status: "done", CreatedAt: store.Now(), Parts: []contract.Part{}}
	event := contract.Event{ConversationID: job.Conversation, Type: "message.end", MessageID: &id, AgentID: &job.Owner}
	if attachment != nil {
		message.Parts = append(message.Parts, contract.Part{Kind: "attachment", Attachment: attachment})
		_, err = s.Store.PublishAttachment(ctx, *attachment, path, event, &message)
	} else {
		text := "Video generation finished without a video. The request has not been resubmitted."
		message.Parts = append(message.Parts, contract.Part{Kind: "text", Text: &text})
		_, err = s.Store.Append(ctx, event, &message)
	}
	return err
}
