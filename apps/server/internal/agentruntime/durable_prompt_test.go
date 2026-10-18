package agentruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/durable/storage"
)

func promptText(t *testing.T, body string) string {
	t.Helper()
	var messages []struct {
		Role    string
		Content json.RawMessage
	}
	if err := json.Unmarshal([]byte(body), &messages); err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.Role == "system" || message.Role == "developer" {
			var content string
			if err := json.Unmarshal(message.Content, &content); err == nil {
				return content
			}
			var parts []struct {
				Text string
			}
			if err := json.Unmarshal(message.Content, &parts); err != nil {
				t.Fatal(err)
			}
			for _, part := range parts {
				content += part.Text
			}
			return content
		}
	}
	t.Fatal("request omitted instructions")
	return ""
}

func TestDurablePrompt(t *testing.T) {
	for _, name := range []string{"configured", "intro", "automatic"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "SKILL.md")
			const skill = "Skill marker: inspect the local widget before acting."
			if err := os.WriteFile(path, []byte(skill), 0600); err != nil {
				t.Fatal(err)
			}
			provider, requests := recordingProvider(t)
			runtime := openDurable(t, provider)
			definition := Definition{Agent: contract.Agent{ID: "coach", Name: "Coach", Role: "Guide"}, Scope: "room", Prompt: "Configured policy", Tools: []string{"read", "ls"}, Skills: []string{path}, MCP: []MCPServer{{Name: "unconnected"}}, Intro: name == "intro", Automatic: name == "automatic"}
			if definition.Intro {
				definition.Prompt = "Introduce yourself briefly as Coach."
			}
			session, err := runtime.Open(t.Context(), definition, "")
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if _, err := session.Send(t.Context(), turn("Hello"), nil); err != nil {
				t.Fatal(err)
			}
			seen := requests()
			if len(seen) != 1 {
				t.Fatalf("requests %d, want 1", len(seen))
			}
			prompt := promptText(t, seen[0])
			toolList := `"Tools":["read"]`
			if definition.Intro {
				toolList = `"Tools":null`
			}
			for _, part := range []string{"Polymux policy", `"ID":"coach"`, `"Name":"Coach"`, `"Role":"Guide"`, `"Conversation":"room"`, toolList, definition.Prompt} {
				if !strings.Contains(prompt, part) {
					t.Errorf("prompt omitted %q", part)
				}
			}
			if !strings.Contains(prompt, "Configured MCP connections are unavailable this turn; no MCP tools are exposed.") {
				t.Fatal("prompt omitted the MCP limitation")
			}
			if strings.Contains(prompt, "unconnected") || strings.Contains(prompt, `"ls"`) {
				t.Fatal("prompt listed an unavailable tool or connection")
			}
			if strings.Contains(prompt, skill) == definition.Intro {
				t.Fatal("wrong skill inclusion for intro state")
			}
			if definition.Intro && strings.Contains(prompt, path) {
				t.Fatal("intro included a skill path")
			}
			if strings.Contains(prompt, "This is a bounded automatic turn.") != definition.Automatic {
				t.Fatal("wrong automatic boundary")
			}
		})
	}
}

func TestDurableOffersPairedDevices(t *testing.T) {
	durable, err := NewDurable(t.Context(), t.TempDir(), "http://127.0.0.1:1/v1", "test-model", "test-key", Client{}, storage.NewMemoryStorage())
	if err != nil {
		t.Fatal(err)
	}
	defer durable.Close()
	for _, intro := range []bool{false, true} {
		definition := Definition{Agent: contract.Agent{ID: "coach", Name: "Coach"}, Devices: []Device{{Name: "mini", Host: "mini.local"}}, Intro: intro, Ephemeral: true}
		session, err := durable.Open(t.Context(), definition, "")
		if err != nil {
			t.Fatal(err)
		}
		agent, err := session.(*durableSession).conversation.Agent(t.Context())
		session.Close()
		if err != nil {
			t.Fatal(err)
		}
		if offered := agent.Instructions != nil && strings.Contains(*agent.Instructions, `"Tools":["run_device"]`); offered == intro {
			t.Fatalf("intro=%v offered run_device=%v", intro, offered)
		}
	}
}

func TestDurablePromptResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SKILL.md")
	const before = "Skill A: inspect the amber widget."
	const after = "Skill B: inspect the blue widget."
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	provider, requests := recordingProvider(t)
	runtime := openDurable(t, provider)
	definition := Definition{Agent: contract.Agent{ID: "coach", Name: "Coach"}, Skills: []string{path}}
	session, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal(err)
	}
	conversation := session.Path()
	if _, err := session.Send(t.Context(), turn("Hello"), nil); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(after), 0600); err != nil {
		t.Fatal(err)
	}
	resumed, err := runtime.Open(t.Context(), definition, conversation)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumed.Path() != conversation {
		t.Fatalf("resumed path %q, want %q", resumed.Path(), conversation)
	}
	if _, err := resumed.Send(t.Context(), turn("Continue"), nil); err != nil {
		t.Fatal(err)
	}
	seen := requests()
	if len(seen) != 2 {
		t.Fatalf("requests %d, want 2", len(seen))
	}
	if !strings.Contains(promptText(t, seen[0]), before) {
		t.Fatal("first request omitted the original skill")
	}
	if !strings.Contains(seen[1], after) || strings.Contains(seen[1], before) {
		t.Fatal("resumed request did not replace the original skill")
	}
}
