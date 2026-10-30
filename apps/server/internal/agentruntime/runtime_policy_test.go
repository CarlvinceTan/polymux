package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/skills"
	"github.com/CarlvinceTan/polymux/apps/server/internal/testenv"
)

func TestRestrictedPolicy(t *testing.T) {
	testenv.PiG(t)
	for _, test := range []struct {
		name             string
		intro, automatic bool
		tools            []string
	}{
		{name: "regular", tools: []string{"bash", "read"}},
		{name: "intro", intro: true, tools: []string{"bash", "read"}},
		{name: "automatic-internal", automatic: true},
		{name: "automatic-configured", automatic: true, tools: []string{"bash", "read"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := make(chan map[string]json.RawMessage, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "bad request", 400)
					return
				}
				requests <- body
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"intro\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n")
				fmt.Fprint(w, "data: {\"id\":\"intro\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer provider.Close()
			runtime := openDurable(t, provider)
			ctx, cancel := context.WithCancel(t.Context())
			definition := Definition{Agent: contract.Agent{ID: "a", Name: "Coach", Role: "Guide"},
				Prompt: "Configured policy", Tools: test.tools,
				Intro: test.intro, Automatic: test.automatic, Ephemeral: test.automatic}
			session, err := runtime.Open(ctx, definition, "")
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			defer session.Close()
			cancel()
			text := "Introduce yourself"
			result, err := session.Send(t.Context(), contract.Message{Parts: []contract.Part{{Kind: "text", Text: &text}}}, func(Delta) error { return nil })
			if err != nil || result.Text != "Hello" {
				t.Fatalf("intro/cached context %+v %v", result, err)
			}
			body := <-requests
			var tools []json.RawMessage
			if err := json.Unmarshal(body["tools"], &tools); err != nil && len(body["tools"]) > 0 {
				t.Fatal(err)
			}
			want := len(test.tools)
			if test.intro {
				want = 0
			}
			if len(tools) != want {
				t.Fatal("turn exposed wrong configured tools", len(tools), want)
			}
			message := string(body["messages"])
			wantHelp := !test.intro
			// With read, skills are listed for loading on demand; without it, their text is inline.
			inline := wantHelp && !slices.Contains(test.tools, "read")
			for _, name := range []string{"polymux", "report"} {
				if strings.Contains(message, "name: "+name) != inline {
					t.Fatalf("prompt inlines %s = %v, want %v", name, !inline, inline)
				}
				if strings.Contains(message, `<skill name=\"`+name+`\"`) != (wantHelp && !inline) {
					t.Fatalf("prompt lists %s wrongly", name)
				}
			}
			if inline {
				for _, marker := range []string{skills.IssueRepo, "explicit confirmation in chat", "[REDACTED]"} {
					if !strings.Contains(message, marker) {
						t.Fatalf("bundled report skill omitted %q", marker)
					}
				}
			}
			for _, part := range []string{"Coach", "Guide", "Configured policy", "Polymux policy", "Current UTC time", "Conversation"} {
				if !strings.Contains(message, part) {
					t.Fatalf("prompt omitted %s", part)
				}
			}
			if test.automatic && !strings.Contains(message, "bounded automatic turn") {
				t.Fatal("automatic turn omitted its permission boundary")
			}
		})
	}
}

func TestPromptToolAvailability(t *testing.T) {
	for _, test := range []struct {
		name       string
		definition Definition
		tools      []string
		want       string
	}{
		{name: "intro", definition: Definition{Intro: true}, tools: []string{"update_agent", "recall_memory"}},
		{name: "child", tools: []string{"bash", "show_html"}},
		{name: "voice", definition: Definition{Ephemeral: true}, tools: []string{"recall_history", "recall_memory", "save_memory", "track_work", "manage_routines"}, want: "park that task with track_work"},
		{name: "automatic", definition: Definition{Automatic: true}, tools: []string{"read"}, want: "Advance established goals with the available tools."},
		{name: "review", definition: Definition{Automatic: true}, tools: []string{"finish_review"}, want: "For a review, use finish_review"},
		{name: "integration-repair", tools: []string{"manage_integrations"}, want: "Use available CLI tools or manage_integrations to repair"},
	} {
		t.Run(test.name, func(t *testing.T) {
			prompt, err := buildPrompt(test.definition, test.tools, true, nil)
			if err != nil {
				t.Fatal(err)
			}
			bundled, err := skills.Bundled()
			if err != nil {
				t.Fatal(err)
			}
			policy := strings.ReplaceAll(prompt, bundled, "")
			for _, name := range []string{"update_agent", "create_agent", "create_group_chat", "update_group_chat", "track_work", "send_to_user", "ask_user", "save_memory", "recall_memory", "forget_memory", "recall_history", "manage_routines", "manage_integrations", "manage_tasks", "manage_triggers", "run_device", "finish_review"} {
				// Guidance may only name a tool the turn offers; most tools describe themselves.
				available := !test.definition.Intro && slices.Contains(test.tools, name)
				if strings.Contains(policy, name) && !available {
					t.Errorf("guidance names %s, which this turn doesn't offer", name)
				}
			}
			if test.want != "" && !strings.Contains(policy, test.want) {
				t.Fatalf("offered capability omitted guidance %q", test.want)
			}
			for _, preserved := range []string{"Polymux policy", "Identity", "NO_RESPONSE", "Configured MCP connections are unavailable"} {
				if !strings.Contains(policy, preserved) {
					t.Errorf("policy omitted %q", preserved)
				}
			}
		})
	}
}
