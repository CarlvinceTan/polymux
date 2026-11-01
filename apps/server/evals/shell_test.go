package evals

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func shellAgent() []agentruntime.Definition {
	return []agentruntime.Definition{{
		Agent: contract.Agent{ID: "shell-agent", Name: "Shell", Model: evalModel},
		Tools: []string{"bash"},
	}}
}

func bashReply(id, command string) Response {
	return ToolReply(id, "bash", map[string]any{"command": command})
}

func expectText(want ...string) func(*ChatRequest) error {
	return func(req *ChatRequest) error {
		text := requestText(req)
		for _, part := range want {
			if !strings.Contains(text, part) {
				return fmt.Errorf("model request lacks %q", part)
			}
		}
		return nil
	}
}

// A message sent during a long command reaches the model before the command ends, and its result follows.
func TestSHELL1MessageDuringCommand(t *testing.T) {
	answered := make(chan time.Time, 1)
	finished := make(chan time.Time, 1)
	stamp := func(at chan time.Time, check func(*ChatRequest) error) func(*ChatRequest) error {
		return func(req *ChatRequest) error {
			at <- time.Now()
			return check(req)
		}
	}
	h := Start(t, Options{ID: "SHELL1", Agents: shellAgent(), Steps: []Step{
		Fixed(nil, bashReply("shell1", "echo started; sleep 9; echo finished")),
		Fixed(stamp(answered, func(req *ChatRequest) error {
			if !hasToolResult(req, "shell1", "still running in the background") || !hasToolResult(req, "shell1", "started") {
				return fmt.Errorf("tool result lacks the background notice")
			}
			return expectText("are you there")(req)
		}), TextReply("Yes, I'm here.")),
		Fixed(stamp(finished, expectText("finished", "left running in the background")), TextReply("It finished.")),
	}})
	const agent = "shell-agent"
	if code := h.Send(agent, "shell1-user", "run the long command"); code != 200 && code != 202 {
		t.Fatalf("send status %d", code)
	}
	if code := sendDuring(h, agent, "shell1-steer", "are you there?"); code != 200 && code != 202 {
		t.Fatalf("steer status %d", code)
	}
	run := h.WaitRunID(RunID("shell1-user", agent), 15*time.Second)
	if run.Status != contract.RunStatusCompleted {
		t.Fatalf("run %s, want completed", run.Status)
	}
	first := <-answered
	select {
	case last := <-finished:
		if last.Sub(first) < 500*time.Millisecond {
			t.Fatalf("the command's result came with the answer, %s later", last.Sub(first))
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("no follow-up with the command's result: %v", h.Emulator.Failures())
	}
	for i := 0; i < 50 && len(h.AssistantDone(agent)) < 2; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if err := h.Emulator.Done(); err != nil {
		fail(t, "SHELL1", err.Error())
		return
	}
	pass(t, "SHELL1", "message answered before the command ended, result followed")
}

// Without a waiting message a long command stays in the turn, as it always did.
func TestSHELL2NoMessageStaysInTurn(t *testing.T) {
	h := Start(t, Options{ID: "SHELL2", Agents: shellAgent(), Steps: []Step{
		Fixed(nil, bashReply("shell2", "sleep 6; echo stayed")),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "shell2", "stayed") {
				return fmt.Errorf("tool result lacks the command's output")
			}
			return nil
		}, TextReply("Done.")),
	}})
	if code := h.Send("shell-agent", "shell2-user", "run it"); code != 200 && code != 202 {
		t.Fatalf("send status %d", code)
	}
	h.WaitRunID(RunID("shell2-user", "shell-agent"), 15*time.Second)
	if err := h.Emulator.Done(); err != nil {
		fail(t, "SHELL2", err.Error())
		return
	}
	pass(t, "SHELL2", "stayed in the turn with no message waiting")
}

// Stop ends a command that left its turn, and no follow-up reports it.
func TestSHELL3StopEndsBackgroundCommand(t *testing.T) {
	const marker = "sleep 47.31"
	h := Start(t, Options{ID: "SHELL3", Agents: shellAgent(), Steps: []Step{
		Fixed(nil, bashReply("shell3", marker)),
		Fixed(expectText("wait"), TextReply("Okay.")),
	}})
	const agent = "shell-agent"
	h.Send(agent, "shell3-user", "run it")
	sendDuring(h, agent, "shell3-steer", "wait")
	h.WaitRunID(RunID("shell3-user", agent), 15*time.Second)
	if !running(marker) {
		t.Fatal("the command did not keep running after its turn")
	}
	h.Stop(agent)
	for i := 0; i < 50 && running(marker); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if running(marker) {
		t.Fatal("Stop left the command running")
	}
	time.Sleep(time.Second)
	if err := h.Emulator.Done(); err != nil {
		fail(t, "SHELL3", err.Error())
		return
	}
	pass(t, "SHELL3", "Stop ended the background command quietly")
}

// A restart ends the command, and the agent hears so instead of waiting for it forever.
func TestSHELL4RestartReportsCommand(t *testing.T) {
	const marker = "sleep 53.17"
	h := Start(t, Options{ID: "SHELL4", Agents: shellAgent(), Steps: []Step{
		Fixed(nil, bashReply("shell4", marker)),
		Fixed(expectText("wait"), TextReply("Okay.")),
		Fixed(expectText("failed or was stopped", marker), TextReply("It was stopped by the restart.")),
	}})
	const agent = "shell-agent"
	h.Send(agent, "shell4-user", "run it")
	sendDuring(h, agent, "shell4-steer", "wait")
	h.WaitRunID(RunID("shell4-user", agent), 15*time.Second)
	h.Restart()
	if running(marker) {
		t.Fatal("the restart left the command running")
	}
	for i := 0; i < 100 && h.Emulator.Count() < 3; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if err := h.Emulator.Done(); err != nil {
		fail(t, "SHELL4", err.Error())
		return
	}
	pass(t, "SHELL4", "restart ended the command and told the agent")
}

// sendDuring waits for the run to reach the model, so the message steers it rather than queueing behind it.
func sendDuring(h *Harness, agent, id, text string) int {
	for i := 0; i < 100 && h.Emulator.Count() < 1; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	return h.Send(agent, id, text)
}

func running(command string) bool {
	return exec.Command("pgrep", "-f", "^sleep "+strings.TrimPrefix(command, "sleep ")).Run() == nil
}
