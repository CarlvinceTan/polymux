package evals

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func selfAgent(tools ...string) []agentruntime.Definition {
	return []agentruntime.Definition{{Agent: contract.Agent{ID: agentID, Name: "Assistant", Model: evalModel}, Tools: tools}}
}

func toolsFind(id string) Response {
	return ToolReply(id, "tool_search", map[string]any{"query": "manage_integrations"})
}

// A bot writes a skill in its own folder, registers it, and carries on with it loaded, without another message.
func TestSKILL2BotMakesSkill(t *testing.T) {
	folder := regexp.MustCompile(`skillFolder\\*":\s*\\*"([^"\\]+)`)
	var path string
	body := "---\nname: note-keeper\ndescription: Keep tidy notes.\n---\n\nFile notes under NOTE_KEEPER_MARKER.\n"
	h := Start(t, Options{ID: "SKILL2", Agents: selfAgent("read", "write"), Steps: []Step{
		Fixed(expectText("skill-creator"), toolsFind("skill2-find")),
		Fixed(nil, ToolReply("skill2-list", "manage_integrations", map[string]any{"action": "list"})),
		{Response: func(req *ChatRequest) Response {
			raw, _ := json.Marshal(req.Messages)
			match := folder.FindSubmatch(raw)
			if match == nil {
				return TextReply("no skill folder")
			}
			path = filepath.Join(string(match[1]), "note-keeper", "SKILL.md")
			return ToolReply("skill2-write", "write", map[string]any{"path": path, "content": body})
		}},
		{Response: func(*ChatRequest) Response {
			return ToolReply("skill2-register", "manage_integrations", map[string]any{"action": "skill", "path": path})
		}},
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "skill2-register", "continue automatically") {
				return fmt.Errorf("registration failed")
			}
			return nil
		}, TextReply("Saved.")),
		{Response: func(req *ChatRequest) Response {
			if !strings.Contains(requestText(req), "Keep tidy notes.") || !strings.Contains(requestText(req), "now loaded") {
				return TextReply("skill not loaded")
			}
			return ToolReply("skill2-read", "read", map[string]any{"path": path})
		}},
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "skill2-read", "NOTE_KEEPER_MARKER") {
				return fmt.Errorf("the new skill could not be read")
			}
			return nil
		}, TextReply("Using it.")),
	}})
	if code := h.Send(agentID, "skill2-make", "Make a note-keeping skill"); code != 202 {
		h.die(fmt.Sprint(code))
	}
	finish(t, h, "SKILL2", 7, "a bot made a skill and used it without another message")
}

// A bot connects an MCP server and calls it without another message.
func TestMCPSETUP1BotConnectsServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(answerMCP))
	defer server.Close()
	h := Start(t, Options{ID: "MCPSETUP1", Agents: selfAgent(), Steps: []Step{
		Fixed(nil, toolsFind("mcp1-find")),
		Fixed(nil, ToolReply("mcp1-add", "manage_integrations", map[string]any{"action": "mcp", "name": "notes", "url": server.URL})),
		Fixed(nil, TextReply("Connected.")),
		Fixed(expectTool("notes__echo"), mcpCall("mcp1-call")),
		Fixed(expectResult("mcp1-call"), TextReply("It works.")),
	}})
	if code := h.Send(agentID, "mcp1-add", "Connect the notes server"); code != 202 {
		h.die(fmt.Sprint(code))
	}
	finish(t, h, "MCPSETUP1", 5, "a bot connected an MCP server and called it without another message")
}

// A bot connects an MCP server for another, idle bot, which has it on its next turn.
func TestMCPSETUP2BotSetsUpIdleBot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(answerMCP))
	defer server.Close()
	h := Start(t, Options{ID: "MCPSETUP2", Agents: twoAgents(), Steps: []Step{
		Fixed(nil, toolsFind("mcp2-find")),
		Fixed(nil, ToolReply("mcp2-add", "manage_integrations", map[string]any{"action": "mcp", "name": "notes", "url": server.URL, "agent_id": "b"})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "mcp2-add", "next turn") {
				return fmt.Errorf("setup for the other bot failed")
			}
			return nil
		}, TextReply("Set up.")),
		Fixed(expectTool("notes__echo"), mcpCall("mcp2-call")),
		Fixed(expectResult("mcp2-call"), TextReply("It works.")),
	}})
	if code := h.Send("a", "mcp2-add", "Connect notes for B"); code != 202 {
		h.die(fmt.Sprint(code))
	}
	if run := h.WaitRun("a", "mcp2-add", "a", deadline); run.Status != "completed" {
		h.die("setup run " + run.Status)
	}
	if code := h.Send("b", "mcp2-use", "Use notes"); code != 202 {
		h.die(fmt.Sprint(code))
	}
	finish(t, h, "MCPSETUP2", 5, "a bot set up another bot, which could use it on its next turn")
}

// A bot registers a skill for a busy bot, which reloads by itself once its turn ends.
func TestSKILL3BotSetsUpBusyBot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: shared-skill\ndescription: A skill from a friend.\n---\n\nBody.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opened := make(chan struct{})
	release := make(chan struct{})
	h := Start(t, Options{ID: "SKILL3", Agents: twoAgents(), Steps: []Step{
		Fixed(nil, HoldReply("Finished my work.", release, func() { close(opened) }, nil)),
		Fixed(nil, toolsFind("skill3-find")),
		Fixed(nil, ToolReply("skill3-add", "manage_integrations", map[string]any{"action": "skill", "path": path, "agent_id": "b"})),
		Fixed(nil, TextReply("Set up.")),
		Fixed(expectText("A skill from a friend.", "now loaded"), TextReply("Thanks.")),
	}})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	if code := h.Send("b", "skill3-work", "Do your work"); code != 202 {
		h.die(fmt.Sprint(code))
	}
	WaitHold(t, "SKILL3", opened, holdWait)
	if code := h.Send("a", "skill3-add", "Give B the skill"); code != 202 {
		h.die(fmt.Sprint(code))
	}
	if run := h.WaitRun("a", "skill3-add", "a", deadline); run.Status != "completed" {
		h.die("setup run " + run.Status)
	}
	close(release)
	finish(t, h, "SKILL3", 5, "a busy bot reloaded by itself after its turn")
}

func twoAgents() []agentruntime.Definition {
	return []agentruntime.Definition{
		{Agent: contract.Agent{ID: "a", Name: "A", Model: evalModel}, Tools: []string{"read"}},
		{Agent: contract.Agent{ID: "b", Name: "B", Model: evalModel}, Tools: []string{"read"}},
	}
}

func mcpCall(id string) Response {
	return ToolReply(id, "codemode", map[string]any{"code": "text(JSON.stringify(await tools.notes__echo({})))"})
}

func expectTool(name string) func(*ChatRequest) error {
	return func(req *ChatRequest) error {
		if !strings.Contains(string(req.Tools), name) {
			return fmt.Errorf("%s is not offered", name)
		}
		return nil
	}
}

func expectResult(id string) func(*ChatRequest) error {
	return func(req *ChatRequest) error {
		if !hasToolResult(req, id, "tool reply") {
			m, _ := toolMessage(req, id)
			return fmt.Errorf("the connected tool did not answer: %s", contentText(m.Content))
		}
		return nil
	}
}

// finish waits for every model request a scenario expects, since a reload needs no message.
func finish(t *testing.T, h *Harness, id string, requests int, reason string) {
	t.Helper()
	for i := 0; i < 150 && h.Emulator.Count() < requests; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if err := h.Emulator.Done(); err != nil {
		fail(t, id, err.Error())
		return
	}
	pass(t, id, reason)
}

func answerMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any = map[string]any{}
	switch request.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": "2025-03-26", "serverInfo": map[string]string{"name": "test", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
	case "tools/list":
		result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "Echo text", "inputSchema": map[string]any{"type": "object"}}}}
	case "tools/call":
		result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "tool reply"}}, "isError": false}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
}
