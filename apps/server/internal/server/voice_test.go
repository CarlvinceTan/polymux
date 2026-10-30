package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/packages/memory"
	"github.com/MichaelKinsy/PiG/agent"
)

type voiceFactory struct {
	mu     sync.Mutex
	opened []agentruntime.Definition
	paths  []string
	script func(context.Context, agentruntime.Definition, contract.Message) (agentruntime.Result, error)
	// deltas stream before the script's result.
	deltas []string
}

func (f *voiceFactory) Open(_ context.Context, d agentruntime.Definition, path string) (agentruntime.Session, error) {
	f.mu.Lock()
	f.opened = append(f.opened, d)
	f.paths = append(f.paths, path)
	f.mu.Unlock()
	return &voiceSession{factory: f, definition: d}, nil
}

type voiceSession struct {
	factory    *voiceFactory
	definition agentruntime.Definition
}

func (s *voiceSession) Send(ctx context.Context, m contract.Message, emit func(agentruntime.Delta) error) (agentruntime.Result, error) {
	for _, text := range s.factory.deltas {
		if err := emit(agentruntime.Delta{Text: text}); err != nil {
			return agentruntime.Result{}, err
		}
	}
	if s.factory.script != nil {
		return s.factory.script(ctx, s.definition, m)
	}
	if err := emit(agentruntime.Delta{Text: "Private result"}); err != nil {
		return agentruntime.Result{}, err
	}
	return agentruntime.Result{Text: "Private result"}, nil
}
func (*voiceSession) Abort(context.Context) error { return nil }
func (*voiceSession) Close() error                { return nil }
func (*voiceSession) Path() string                { return "must-not-be-saved.jsonl" }

func voiceTest(t *testing.T, factory *voiceFactory, options ...func(*Config)) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	definitions := []agentruntime.Definition{
		{Agent: contract.Agent{ID: "a", Name: "A", Model: "test/model"}, Prompt: "Existing permission policy", Tools: []string{"read", "bash"}},
		{Agent: contract.Agent{ID: "b", Name: "B"}},
	}
	config := Config{DataDir: dir, Definitions: definitions, Factory: factory, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, option := range options {
		option(&config)
	}
	api, err := New(t.Context(), db, config)
	if err != nil {
		t.Fatal(err)
	}
	web := httptest.NewServer(api)
	t.Cleanup(func() { web.Close(); api.Close(); db.Close() })
	return api, web
}

func voicePost(t *testing.T, web *httptest.Server, thread, id string) (int, contract.VoiceResponse) {
	t.Helper()
	body, err := json.Marshal(contract.VoiceRequest{ID: id, Text: "A private spoken request"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(web.URL+"/threads/"+thread+"/voice", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result contract.VoiceResponse
	if response.StatusCode == 200 {
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, result
}

func privateChat(t *testing.T, api *Server, threadID string) {
	t.Helper()
	messages, err := api.Store.Messages(t.Context(), threadID, 0, 100)
	if err != nil || len(messages) != 0 {
		t.Fatalf("private speech leaked into messages: %+v %v", messages, err)
	}
	events, err := api.Store.Events(t.Context(), threadID, 0, 100)
	if err != nil || len(events) != 0 {
		t.Fatalf("private speech leaked into events: %+v %v", events, err)
	}
	thread, err := api.Store.Thread(t.Context(), threadID)
	if err != nil || thread.LastSeq != 0 || thread.UnreadCount != 0 {
		t.Fatalf("private speech changed typed chat: %+v %v", thread, err)
	}
}

func TestVoicePrivacyAndPermissions(t *testing.T) {
	t.Parallel()
	factory := &voiceFactory{}
	api, web := voiceTest(t, factory)
	status, result := voicePost(t, web, "a", "voice-one")
	if status != 200 || !strings.Contains(result.Text, "Private result") {
		t.Fatalf("voice response %d %+v", status, result)
	}
	privateChat(t, api, "a")
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.opened) != 1 || !factory.opened[0].Ephemeral || factory.paths[0] != "" {
		t.Fatalf("voice reused durable sessions: %+v %v", factory.opened, factory.paths)
	}
	d := factory.opened[0]
	if d.Agent.Model != "test/model" || !slices.Equal(d.Tools, []string{"read", "bash"}) || !strings.HasPrefix(d.Prompt, "Existing permission policy") || d.BeforeTool == nil {
		t.Fatalf("voice changed existing agent policy: %+v", d)
	}
	// Voice may keep a private run/job for the approval ledger; chat surfaces must stay empty.
	for _, table := range []string{"messages", "events", "sessions", "group_sessions", "deliveries"} {
		var count int
		if err := api.Store.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("private execution persisted in %s: %d %v", table, count, err)
		}
	}
}

func TestVoiceMemoryScope(t *testing.T) {
	for _, mode := range []string{"peer", "group"} {
		t.Run(mode, func(t *testing.T) {
			var group string
			factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, message contract.Message) (agentruntime.Result, error) {
				if d.TurnContext == nil {
					return agentruntime.Result{}, fmt.Errorf("private tools lost their turn context")
				}
				restored, ok := d.TurnContext()
				if !ok || restored.Value(turnKey{}) == nil || restored.Value(voiceKey{}) == nil || restored.Value(htmlKey{}) == nil {
					return agentruntime.Result{}, fmt.Errorf("private tool context is incomplete")
				}
				state := restored.Value(turnKey{}).(*turnState)
				want := d.Agent.ID + " private fact"
				other := "b private fact"
				if d.Agent.ID == "b" {
					other = "a private fact"
				}
				if mode == "group" {
					want, other = "group fact", "private fact"
					if state.run.ConversationID != group {
						return agentruntime.Result{}, fmt.Errorf("group consultation changed memory scope")
					}
				} else if state.run.ConversationID != d.Agent.ID {
					return agentruntime.Result{}, fmt.Errorf("peer consultation used the sender's memory scope")
				}
				if text := store.Text(message); !strings.Contains(text, want) || strings.Contains(text, other) {
					return agentruntime.Result{}, fmt.Errorf("private context lost or mixed scoped memory")
				}
				recalled, err := botCall(restored, d, "recall_memory", `{}`)
				if err != nil || !strings.Contains(recalled.Text(), want) || strings.Contains(recalled.Text(), other) {
					return agentruntime.Result{}, fmt.Errorf("private tool lost or mixed scoped memory: %v", err)
				}
				if d.Agent.ID == "a" {
					name, args := "message_agent", `{"agent_id":"b","message":"Consult privately","intent":"request"}`
					if mode == "group" {
						name, args = "handoff_agent", `{"agent_id":"b","message":"Consult privately"}`
					}
					_, err = botCall(restored, d, name, args)
				}
				return agentruntime.Result{Text: "Private result"}, err
			}}
			api, web := voiceTest(t, factory)
			for _, id := range []string{"a", "b"} {
				if _, err := api.memory.Save(t.Context(), id, id, memory.Document{Name: "private", Text: id + " private fact"}); err != nil {
					t.Fatal(err)
				}
			}
			thread := "a"
			if mode == "group" {
				created, err := api.newGroup(t.Context(), contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}})
				if err != nil {
					t.Fatal(err)
				}
				group, thread = created.ID, created.ID
				for _, id := range []string{"a", "b"} {
					if _, err := api.memory.Save(t.Context(), id, group, memory.Document{Name: "shared", Text: "group fact"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if status, _ := voicePost(t, web, thread, "scoped-voice"); status != http.StatusOK {
				t.Fatal("private consultation failed", status)
			}
			factory.mu.Lock()
			defer factory.mu.Unlock()
			if len(factory.opened) != 2 {
				t.Fatal("consultation did not reach the peer")
			}
		})
	}
}

func TestVoiceRefusesReplay(t *testing.T) {
	t.Parallel()
	factory := &voiceFactory{}
	api, web := voiceTest(t, factory)
	if status, _ := voicePost(t, web, "a", "same"); status != 200 {
		t.Fatal(status)
	}
	if status, _ := voicePost(t, web, "a", "same"); status != 409 {
		t.Fatal("replayed private task", status)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.opened) != 1 {
		t.Fatal("duplicate effects", len(factory.opened))
	}
	privateChat(t, api, "a")
}

func TestVoiceExplicitPublication(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"text", "html"} {
		t.Run(mode, func(t *testing.T) {
			factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
				name, args := "send_to_user", json.RawMessage(`{"text":"A deliberate user-facing note"}`)
				if mode == "html" {
					name, args = "show_html", json.RawMessage(`{"html":"<h1>Useful result</h1>","title":"Result"}`)
				}
				for _, tool := range d.ExtraTools {
					if tool.Name() == name {
						for range 2 {
							if _, err := tool.Execute(ctx, "publish-one", args, nil); err != nil {
								return agentruntime.Result{}, err
							}
						}
					}
				}
				return agentruntime.Result{Text: "Private findings for the spokesperson"}, nil
			}}
			api, web := voiceTest(t, factory)
			if status, _ := voicePost(t, web, "a", "publish"); status != 200 {
				t.Fatal(status)
			}
			messages, err := api.Store.Messages(t.Context(), "a", 0, 100)
			if err != nil || len(messages) != 1 || messages[0].Role != "assistant" || messages[0].ReplyToID != nil {
				t.Fatalf("publication %+v %v", messages, err)
			}
			if strings.Contains(store.Text(messages[0]), "Private") || messages[0].AgentID == nil || *messages[0].AgentID != "a" {
				t.Fatal("publication exposed private findings or lost sender")
			}
		})
	}
}

func TestVoicePrivatePeerAndGroupHandoff(t *testing.T) {
	t.Parallel()
	for _, group := range []bool{false, true} {
		name := "peer"
		if group {
			name = "group"
		}
		t.Run(name, func(t *testing.T) {
			pairings, err := device.Open(t.Context(), t.TempDir(), "127.0.0.1")
			if err != nil {
				t.Fatal(err)
			}
			broker := device.New(pairings)
			t.Cleanup(func() { pairings.Close() })
			factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
				if d.Agent.ID == "a" {
					toolName, args := "message_agent", json.RawMessage(`{"agent_id":"b","message":"Private delegated question","intent":"question"}`)
					if group {
						toolName, args = "handoff_agent", json.RawMessage(`{"agent_id":"b","message":"Private group task"}`)
					}
					for _, tool := range d.ExtraTools {
						if tool.Name() == toolName {
							if _, err := tool.Execute(ctx, "dispatch", args, nil); err != nil {
								return agentruntime.Result{}, err
							}
						}
					}
				}
				return agentruntime.Result{Text: "Private result from " + d.Agent.ID}, nil
			}}
			api, web := voiceTest(t, factory, func(config *Config) { config.Devices = broker })
			thread := "a"
			if group {
				created, err := api.Store.CreateThread(t.Context(), "team", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}})
				if err != nil {
					t.Fatal(err)
				}
				thread = created.ID
			}
			status, result := voicePost(t, web, thread, "private-coordination")
			if status != 200 || !strings.Contains(result.Text, "Private result from b") {
				t.Fatalf("did not wait for private descendant: %d %+v", status, result)
			}
			factory.mu.Lock()
			opened := slices.Clone(factory.opened)
			factory.mu.Unlock()
			if len(opened) != 2 {
				t.Fatal("voice scope probe did not open both agents")
			}
			privateChat(t, api, "a")
			privateChat(t, api, "b")
			if group {
				privateChat(t, api, thread)
			}
		})
	}
}

func TestVoiceCancellationIsPrivate(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	finished := make(chan struct{})
	factory := &voiceFactory{script: func(ctx context.Context, _ agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return agentruntime.Result{}, ctx.Err()
	}}
	api, web := voiceTest(t, factory)
	ctx, cancel := context.WithCancel(t.Context())
	request, _ := http.NewRequestWithContext(ctx, "POST", web.URL+"/threads/a/voice", strings.NewReader(`{"id":"cancel","text":"Private work"}`))
	request.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("voice work did not start")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("voice work did not cancel")
	}
	<-done
	privateChat(t, api, "a")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT status FROM runs WHERE thread_id='a'`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "cancelled" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cancelled voice consultation remained active")
}

var _ agent.AgentTool = (*voicePublish)(nil)
