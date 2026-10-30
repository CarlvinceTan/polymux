package evals

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// A skill is listed by location, so the agent must be able to read it there.
func TestSKILL1ListedSkillIsReadable(t *testing.T) {
	location := regexp.MustCompile(`<skill name=\\?"html-cards\\?" location=\\?"([^"\\]+)\\?"`)
	h := Start(t, Options{ID: "SKILL1", Agents: []agentruntime.Definition{{
		Agent: contract.Agent{ID: agentID, Name: "Assistant", Model: evalModel}, Tools: []string{"read"},
	}}, Steps: []Step{
		{Response: func(req *ChatRequest) Response {
			match := location.FindStringSubmatch(requestText(req))
			if match == nil {
				return TextReply("no skill listed")
			}
			return ToolReply("skill1-read", "read", map[string]any{"path": match[1]})
		}},
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "skill1-read", "polymuxSend") {
				return fmt.Errorf("the html-cards skill could not be read at its listed location")
			}
			return nil
		}, TextReply("done")),
	}})
	if code := h.Send(agentID, "skill1-user", "Build a small card"); code != 202 {
		h.die(fmt.Sprint(code))
	}
	if run := h.WaitRun(agentID, "skill1-user", agentID, deadline); run.Status != "completed" {
		h.die("run " + run.Status)
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	if strings.Contains(requestText(h.Emulator.requests[0]), "## Kit") {
		h.die("skill text was inlined for an agent that can read it")
	}
	pass(t, "SKILL1", "listed skills load from their location")
}
