package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type profileFactory struct {
	opened chan openedAgent
	first  chan struct{}
	count  atomic.Int32
	closed atomic.Int32
	unsafe atomic.Bool
}

func (f *profileFactory) Open(_ context.Context, d agentruntime.Definition, path string) (agentruntime.Session, error) {
	if d.Intro {
		return &testSession{factory: &testFactory{}}, nil
	}
	first := f.count.Add(1) == 1
	f.opened <- openedAgent{definition: d, path: path}
	return &profileSession{factory: f, definition: d, first: first}, nil
}

type profileSession struct {
	factory    *profileFactory
	definition agentruntime.Definition
	first      bool
	active     atomic.Bool
}

func (s *profileSession) Send(ctx context.Context, _ contract.Message, emit func(agentruntime.Delta) error) (agentruntime.Result, error) {
	s.active.Store(true)
	defer s.active.Store(false)
	if s.first {
		select {
		case <-s.factory.first:
		case <-ctx.Done():
			return agentruntime.Result{}, ctx.Err()
		}
	}
	text := s.definition.SystemPrompt()
	if err := emit(agentruntime.Delta{Text: text}); err != nil {
		return agentruntime.Result{}, err
	}
	return agentruntime.Result{Text: text}, nil
}

func (*profileSession) Abort(context.Context) error { return nil }
func (*profileSession) Path() string                { return "profile.jsonl" }
func (s *profileSession) Close() error {
	if s.active.Load() {
		s.factory.unsafe.Store(true)
	}
	s.factory.closed.Add(1)
	return nil
}

func profileRequest(t *testing.T, api *Server, method, path, body string, status int) contract.AgentProfile {
	t.Helper()
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	if recorder.Code != status {
		t.Fatalf("profile request %s %s status=%d body=%s", method, path, recorder.Code, recorder.Body.String())
	}
	var profile contract.AgentProfile
	if status == 200 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &profile); err != nil {
			t.Fatal(err)
		}
	}
	return profile
}

func profileReply(t *testing.T, api *Server, thread, user string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := api.Store.Messages(t.Context(), thread, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range messages {
			if m.ReplyToID != nil && *m.ReplyToID == user && m.Status == "done" {
				return store.Text(m)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("profile reply did not finish")
	return ""
}

func TestProfileEditAppliesAfterActiveReply(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(t.Context(), filepath.Join(dir, "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	factory := &profileFactory{opened: make(chan openedAgent, 4), first: make(chan struct{})}
	api, err := New(t.Context(), s, Config{DataDir: dir, Model: "provider/default", Factory: factory, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	created := httptest.NewRecorder()
	api.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(`{"name":"Random name","role":"","prompt":""}`)))
	var agent contract.Agent
	if err := json.Unmarshal(created.Body.Bytes(), &agent); err != nil || created.Code != 201 || agent.Role != "" {
		t.Fatalf("blank-role creation %+v status=%d err=%v", agent, created.Code, err)
	}
	path := "/agents/" + agent.ID
	initial := profileRequest(t, api, http.MethodGet, path, "", 200)
	if initial.Prompt != "" || initial.Agent.Role != "" {
		t.Fatalf("creation assigned instructions %+v", initial)
	}
	send := func(id string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		body := `{"id":"` + id + `","text":"hello","attachmentIDs":[]}`
		api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/"+agent.ID+"/messages", strings.NewReader(body)))
		if recorder.Code != 202 {
			t.Fatalf("send status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	send("before")
	first := <-factory.opened
	updated := profileRequest(t, api, http.MethodPatch, path, `{"name":"Edited","role":"Coach","prompt":"Use short sentences."}`, 200)
	if !updated.Agent.Active || updated.Prompt != "Use short sentences." || updated.Agent.Model != agent.Model {
		t.Fatalf("active profile/model lost %+v", updated)
	}
	if factory.closed.Load() != 0 {
		t.Fatal("editing closed the active session")
	}
	avatar := avatarDesign()
	identity := avatarRequest(t, api, path+"/avatar", avatar, 200)
	if !identity.Active || factory.closed.Load() != 0 {
		t.Fatal("avatar change disturbed the active reply")
	}
	profile := profileRequest(t, api, http.MethodGet, path, "", 200)
	if profile.Prompt != updated.Prompt || profile.Agent.Role != updated.Agent.Role {
		t.Fatal("avatar change overwrote the concurrently edited profile")
	}
	close(factory.first)
	if reply := profileReply(t, api, agent.ID, "before"); reply != first.definition.SystemPrompt() {
		t.Fatalf("active reply changed instructions: %q", reply)
	}
	send("after")
	if reply := profileReply(t, api, agent.ID, "after"); reply != updated.Prompt {
		t.Fatalf("next reply ignored edited instructions: %q", reply)
	}
	second := <-factory.opened
	if second.path != "profile.jsonl" || second.definition.Agent.ID != agent.ID || len(second.definition.Tools) != len(first.definition.Tools) || factory.closed.Load() != 1 || factory.unsafe.Load() {
		t.Fatalf("profile reload lost continuity %+v closed=%d unsafe=%v", second, factory.closed.Load(), factory.unsafe.Load())
	}
	cleared := profileRequest(t, api, http.MethodPatch, path, `{"name":"Edited","role":"","prompt":""}`, 200)
	if cleared.Agent.Role != "" || cleared.Prompt != "" {
		t.Fatalf("could not clear profile %+v", cleared)
	}
	send("cleared")
	if reply := profileReply(t, api, agent.ID, "cleared"); reply != (agentruntime.Definition{}).SystemPrompt() {
		t.Fatalf("cleared profile did not use generic fallback: %q", reply)
	}
	threads, err := s.Threads(t.Context())
	messages, messageErr := s.Messages(t.Context(), agent.ID, 0, 100)
	if err != nil || messageErr != nil || len(threads) != 1 || len(messages) != 7 {
		t.Fatalf("profile edit changed history/conversation threads=%d messages=%d err=%v/%v", len(threads), len(messages), err, messageErr)
	}
}

func TestEditedDefaultProfileSurvivesStartupSeeding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "polymux.db")
	config := Config{DataDir: dir, Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant", Role: "Original", Model: "provider/default"}, Prompt: "Original instructions", Tools: []string{"read", "bash"}}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(t.Context(), s, config)
	if err != nil {
		t.Fatal(err)
	}
	profileRequest(t, api, http.MethodPatch, "/agents/assistant", `{"name":"My assistant","role":"","prompt":"New instructions"}`, 200)
	api.Close()
	s.Close()
	s, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api, err = New(t.Context(), s, config)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	profile := profileRequest(t, api, http.MethodGet, "/agents/assistant", "", 200)
	if profile.Agent.Name != "My assistant" || profile.Agent.Role != "" || profile.Prompt != "New instructions" || profile.Agent.Model != "provider/default" {
		t.Fatalf("startup replaced user profile %+v", profile)
	}
	definition, err := s.Definition(t.Context(), "assistant")
	var d agentruntime.Definition
	decodeErr := json.Unmarshal(definition, &d)
	if err != nil || decodeErr != nil || len(d.Tools) != 2 || d.Tools[0] != "read" || d.Tools[1] != "bash" {
		t.Fatalf("profile edit changed tools %+v err=%v/%v", d, err, decodeErr)
	}
	profileRequest(t, api, http.MethodGet, "/agents/missing", "", 404)
	profileRequest(t, api, http.MethodPatch, "/agents/missing", `{"name":"Missing","role":"","prompt":""}`, 404)
	profileRequest(t, api, http.MethodPatch, "/agents/assistant", `{"name":"","role":"","prompt":""}`, 400)
	profileRequest(t, api, http.MethodPatch, "/agents/assistant", `{"name":"Valid","role":"","prompt":"","model":"other"}`, 400)
}
