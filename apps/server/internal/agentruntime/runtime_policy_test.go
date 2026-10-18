package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
			wantHelp := !test.intro && !test.automatic
			for _, marker := range []string{"name: polymux", "name: report"} {
				if strings.Contains(message, marker) != wantHelp {
					t.Fatalf("prompt contains %s = %v, want %v", marker, strings.Contains(message, marker), wantHelp)
				}
			}
			if wantHelp {
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

func TestUnavailableMCPReturnsNonReusableFallback(t *testing.T) {
	testenv.PiG(t)
	runtime := openDurable(t, &httptest.Server{URL: "http://127.0.0.1:1"})
	definition := Definition{Agent: contract.Agent{ID: "a"}, Tools: []string{"read"}, MCP: []MCPServer{{Name: "unavailable"}}}
	session, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal("CLI fallback failed", err)
	}
	defer session.Close()
	if session.(*durableSession).Reusable() {
		t.Fatal("missing MCP will never be retried")
	}
	definition.MCP = nil
	healthy, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal(err)
	}
	defer healthy.Close()
	if !healthy.(*durableSession).Reusable() {
		t.Fatal("healthy session not reusable")
	}
}
