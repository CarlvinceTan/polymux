package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/skills"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/durable/storage"
)

// recordingProvider answers every turn and records the request bodies it saw.
func recordingProvider(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	bodies := []string{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		encoded, _ := json.Marshal(body["messages"])
		mu.Lock()
		bodies = append(bodies, string(encoded))
		mu.Unlock()
		sse(w, "Noted.", nil)
	}))
	t.Cleanup(provider.Close)
	return provider, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

func turn(text string) contract.Message {
	return contract.Message{ID: "m-" + text, ThreadID: "assistant", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}}}
}

func openDurable(t *testing.T, provider *httptest.Server) *DurableRuntime {
	t.Helper()
	runtime, err := NewDurable(t.Context(), t.TempDir(), provider.URL+"/v1", "test-model", "test-key", Client{}, storage.NewMemoryStorage())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime
}

func TestDurableHelpInstructions(t *testing.T) {
	for _, tc := range []struct {
		name             string
		intro, automatic bool
	}{
		{name: "regular"},
		{name: "intro", intro: true},
		{name: "automatic", automatic: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := openDurable(t, &httptest.Server{URL: "http://localhost"})
			definition := Definition{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}, Intro: tc.intro, Automatic: tc.automatic}
			session, err := runtime.Open(t.Context(), definition, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			state, err := session.(*durableSession).conversation.Agent(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if state.Instructions == nil {
				t.Fatal("conversation has no instructions")
			}
			want, err := buildPrompt(definition, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			if *state.Instructions != want {
				t.Error("conversation instructions do not match the configured prompt")
			}
			wantSkills := !tc.intro && !tc.automatic
			markers := []string{
				"name: polymux",
				"name: report",
				skills.IssueRepo,
				"explicit confirmation in chat",
				"[REDACTED]",
			}
			for _, marker := range markers {
				if got := strings.Contains(*state.Instructions, marker); got != wantSkills {
					t.Errorf("instructions contain %q = %v, want %v", marker, got, wantSkills)
				}
			}
		})
	}
}

func TestDurableSessionRunsAndResumesByPath(t *testing.T) {
	provider, requests := recordingProvider(t)
	runtime := openDurable(t, provider)
	definition := Definition{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}}
	session, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal(err)
	}
	path := session.Path()
	if !strings.HasPrefix(path, "durable:") {
		t.Fatalf("unexpected path %q", path)
	}
	result, err := session.Send(t.Context(), turn("remember ALPHA"), nil)
	if err != nil || result.Text != "Noted." {
		t.Fatalf("turn %+v %v", result, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	// A second session on the same path resumes the same conversation, so the earlier
	// turn is still in the model's context.
	resumed, err := runtime.Open(t.Context(), definition, path)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Path() != path {
		t.Fatalf("resumed path %q want %q", resumed.Path(), path)
	}
	if _, err := resumed.Send(t.Context(), turn("what should you remember"), nil); err != nil {
		t.Fatal(err)
	}
	seen := requests()
	if len(seen) != 2 {
		t.Fatalf("requests %d", len(seen))
	}
	if strings.Contains(seen[0], "what should you remember") {
		t.Fatal("first request already carried the later turn")
	}
	if !strings.Contains(seen[1], "ALPHA") {
		t.Fatalf("resumed conversation lost its context: %s", seen[1])
	}
}

func TestEphemeralDurableSessionStartsFresh(t *testing.T) {
	provider, requests := recordingProvider(t)
	runtime := openDurable(t, provider)
	definition := Definition{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}}
	session, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal(err)
	}
	path := session.Path()
	if _, err := session.Send(t.Context(), turn("remember ALPHA"), nil); err != nil {
		t.Fatal(err)
	}
	// An ephemeral turn is handed a path but must not resume it: a voice turn or an
	// automatic review cannot inherit another turn's context.
	ephemeral := definition
	ephemeral.Ephemeral = true
	fresh, err := runtime.Open(t.Context(), ephemeral, path)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Path() == path {
		t.Fatal("ephemeral turn resumed an existing conversation")
	}
	if _, err := fresh.Send(t.Context(), turn("anything"), nil); err != nil {
		t.Fatal(err)
	}
	seen := requests()
	if len(seen) != 2 || strings.Contains(seen[1], "ALPHA") {
		t.Fatalf("ephemeral turn inherited context: %v", seen)
	}
}

func TestDurableExtensionsUnregisterOnClose(t *testing.T) {
	runtime := openDurable(t, &httptest.Server{URL: "http://localhost"})
	baseline := len(runtime.registry.Snapshot().Installed())
	definition := Definition{
		Agent:      contract.Agent{ID: "assistant", Name: "Assistant"},
		Tools:      []string{"read"},
		ExtraTools: []agent.AgentTool{&stateTool{}},
		BeforeTool: func(context.Context, string, string, json.RawMessage) agent.ToolCallHookResult {
			return agent.ToolCallHookResult{}
		},
	}
	for range 3 {
		first, err := runtime.Open(t.Context(), definition, "")
		if err != nil {
			t.Fatal(err)
		}
		second, err := runtime.Open(t.Context(), definition, "")
		if err != nil {
			t.Fatal(err)
		}
		if count := len(runtime.registry.Snapshot().Installed()); count != baseline+6 {
			t.Fatalf("registered extensions %d, want %d", count, baseline+6)
		}
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		if count := len(runtime.registry.Snapshot().Installed()); count != baseline+3 {
			t.Fatalf("closing one session left %d extensions, want %d", count, baseline+3)
		}
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		if err := second.Close(); err != nil {
			t.Fatal(err)
		}
		if count := len(runtime.registry.Snapshot().Installed()); count != baseline {
			t.Fatalf("closing sessions left %d extensions, want baseline %d", count, baseline)
		}
		resumed, err := runtime.Open(t.Context(), definition, first.Path())
		if err != nil {
			t.Fatal(err)
		}
		if count := len(runtime.registry.Snapshot().Installed()); count != baseline+3 {
			t.Fatalf("resumed session registered %d extensions, want %d", count, baseline+3)
		}
		if err := resumed.Close(); err != nil {
			t.Fatal(err)
		}
		if count := len(runtime.registry.Snapshot().Installed()); count != baseline {
			t.Fatal("resumed extensions leaked")
		}
	}
}
