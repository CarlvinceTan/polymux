package evals

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/agent"
)

// await holds a step until the test releases it, and gives up so a failed test can't hang the emulator.
func await(release <-chan struct{}) {
	select {
	case <-release:
	case <-time.After(holdWait):
	}
}

// watchAnswers delivers each send_to_user message from the stream the app reads, and the run's status when it arrived.
func watchAnswers(t *testing.T, h *Harness, thread, run string) <-chan string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.HTTP.URL+"/threads/"+thread+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	answers := make(chan string, 8)
	go func() {
		defer response.Body.Close()
		lines := bufio.NewScanner(response.Body)
		lines.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for lines.Scan() {
			data, ok := strings.CutPrefix(lines.Text(), "data: ")
			if !ok {
				continue
			}
			var event contract.Event
			if json.Unmarshal([]byte(data), &event) != nil || event.Message == nil || !strings.HasPrefix(event.Message.ID, "send_to_user:") {
				continue
			}
			state, _ := h.Store.Run(ctx, run)
			answers <- state.Status + ": " + messageText(*event.Message)
		}
	}()
	return answers
}

func expectSteer(text string) func(*ChatRequest) error {
	return func(req *ChatRequest) error {
		_, latest := latestUser(req)
		if !strings.Contains(latest, text) || !strings.Contains(latest, agentruntime.SteerNote) {
			return fmt.Errorf("latest user message lacks the steer and its note: %q", latest)
		}
		return nil
	}
}

// The owner's message joins the next step of a busy run, its answer reaches the app mid-run, and the work goes on.
func TestSTEER1AnswerDuringRun(t *testing.T) {
	const assistant = "assistant"
	ran := &atomic.Int32{}
	opened, release, working, finish := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	h := Start(t, Options{ID: "STEER1", Extras: map[string][]agent.AgentTool{assistant: {&typedTool{ran: ran}}}, Steps: []Step{
		{Response: func(*ChatRequest) Response {
			close(opened)
			await(release)
			return ToolReply("steer1-first", "typed_tool", map[string]any{"value": "first"})
		}},
		Fixed(expectSteer("are you there?"), ToolReply("steer1-answer", "send_to_user", map[string]any{"text": "Yes, on it."})),
		{Response: func(req *ChatRequest) Response {
			if !hasToolResult(req, "steer1-answer", "posted") {
				t.Error("the next step lacks the answer's result")
			}
			close(working)
			await(finish)
			return ToolReply("steer1-second", "typed_tool", map[string]any{"value": "second"})
		}},
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "steer1-first", "typed-ok") || !hasToolResult(req, "steer1-second", "typed-ok") {
				return fmt.Errorf("the run lost its earlier steps")
			}
			return nil
		}, TextReply("Both steps are done.")),
	}})
	id := RunID("steer1-user", assistant)
	if code := h.Send(assistant, "steer1-user", "do both steps"); code != 200 && code != 202 {
		t.Fatalf("send status %d", code)
	}
	answers := watchAnswers(t, h, assistant, id)
	WaitHold(t, "STEER1", opened, holdWait)
	if code := h.Send(assistant, "steer1-steer", "are you there?"); code != 200 && code != 202 {
		t.Fatalf("steer status %d", code)
	}
	close(release)
	WaitHold(t, "STEER1", working, holdWait)
	select {
	case answer := <-answers:
		if answer != "running: Yes, on it." {
			t.Fatalf("answer reached the app as %q", answer)
		}
	case <-time.After(holdWait):
		t.Fatal("the answer never reached the app's stream")
	}
	close(finish)
	run := h.WaitRunID(id, deadline)
	if err := h.Emulator.Done(); err != nil {
		fail(t, "STEER1", err.Error())
		return
	}
	var runs int
	if err := h.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM runs WHERE thread_id=?`, assistant).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if run.Status != contract.RunStatusCompleted || runs != 1 || ran.Load() != 2 {
		t.Fatalf("run %s, %d runs, %d of 2 steps", run.Status, runs, ran.Load())
	}
	want := []string{"do both steps", "are you there?", "Yes, on it.", "Both steps are done."}
	got := []string{}
	for _, m := range h.Messages(assistant) {
		got = append(got, messageText(m))
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chat reads %q", got)
	}
	pass(t, "STEER1", "answered live mid-run, then finished both steps in one run")
}

// An answer left to the final reply reads below the message it answers.
func TestSTEER2FinalReplyFollowsMessage(t *testing.T) {
	const assistant = "assistant"
	opened, release := make(chan struct{}), make(chan struct{})
	h := Start(t, Options{ID: "STEER2", Extras: map[string][]agent.AgentTool{assistant: {&typedTool{ran: &atomic.Int32{}}}}, Steps: []Step{
		{Response: func(*ChatRequest) Response {
			close(opened)
			await(release)
			return ToolReply("steer2-tool", "typed_tool", map[string]any{"value": "first"})
		}},
		Fixed(expectSteer("are you there?"), TextReply("Yes, and it's done.")),
	}})
	h.Send(assistant, "steer2-user", "do the job")
	WaitHold(t, "STEER2", opened, holdWait)
	h.Send(assistant, "steer2-steer", "are you there?")
	close(release)
	h.WaitRunID(RunID("steer2-user", assistant), deadline)
	messages := h.Messages(assistant)
	if len(messages) != 3 || messageText(messages[1]) != "are you there?" || messageText(messages[2]) != "Yes, and it's done." {
		t.Fatalf("chat reads %+v", messages)
	}
	if err := h.Emulator.Done(); err != nil {
		fail(t, "STEER2", err.Error())
		return
	}
	pass(t, "STEER2", "final reply sits below the message it answers")
}

// A message the run never took keeps its place after the reply it didn't shape.
func TestSTEER3LateMessageKeepsOrder(t *testing.T) {
	const assistant = "assistant"
	opened, release := make(chan struct{}), make(chan struct{})
	h := Start(t, Options{ID: "STEER3", Steps: []Step{
		Fixed(nil, HoldReply("First answer.", release, func() { close(opened) }, nil)),
		Fixed(nil, TextReply("Second answer.")),
	}})
	h.Send(assistant, "steer3-user", "first")
	WaitHold(t, "STEER3", opened, holdWait)
	h.Send(assistant, "steer3-late", "second")
	close(release)
	h.WaitRun(assistant, "steer3-late", assistant, deadline)
	got := []string{}
	for _, m := range h.Messages(assistant) {
		got = append(got, messageText(m))
	}
	if want := "first|First answer.|second|Second answer."; strings.Join(got, "|") != want {
		t.Fatalf("chat reads %q", got)
	}
	if err := h.Emulator.Done(); err != nil {
		fail(t, "STEER3", err.Error())
		return
	}
	pass(t, "STEER3", "late message and its answer follow the first reply")
}

// A bot's message waits for the run, because its answer returns as that message's own receipt.
func TestSTEER4PeerMessageWaits(t *testing.T) {
	const worker, sender = "worker", "sender"
	opened, release, sent := make(chan struct{}), make(chan struct{}), make(chan struct{})
	h := Start(t, Options{ID: "STEER4", Effects: map[string]*atomic.Int32{worker: {}}, Agents: []agentruntime.Definition{
		{Agent: contract.Agent{ID: worker, Name: "Worker", Model: evalModel}, Tools: []string{}},
		{Agent: contract.Agent{ID: sender, Name: "Sender", Model: evalModel}, Tools: []string{}},
	}, Steps: []Step{
		{Response: func(*ChatRequest) Response {
			close(opened)
			await(release)
			return ToolReply("steer4-work", "side_effect", map[string]any{})
		}},
		Fixed(nil, ToolReply("steer4-peer", "message_agent", map[string]any{"agent_id": worker, "message": "PEER_STATUS please", "intent": "question"})),
		{Response: func(*ChatRequest) Response { close(sent); return TextReply("Asked.") }},
		Fixed(func(req *ChatRequest) error {
			if strings.Contains(requestText(req), "PEER_STATUS") {
				return fmt.Errorf("the bot's message reached the busy run")
			}
			return nil
		}, TextReply("Work done.")),
		Fixed(expectText("PEER_STATUS"), TextReply("All clear.")),
		Fixed(expectText("All clear."), TextReply("NO_RESPONSE")),
	}})
	h.Send(worker, "steer4-work", "long job")
	WaitHold(t, "STEER4", opened, holdWait)
	h.Send(sender, "steer4-ask", "ask the worker how it's going")
	WaitHold(t, "STEER4", sent, holdWait)
	close(release)
	h.WaitRun(worker, "steer4-work", worker, deadline)
	for i := 0; i < 500 && h.Emulator.Count() < 6; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if err := h.Emulator.Done(); err != nil {
		fail(t, "STEER4", err.Error())
		return
	}
	pass(t, "STEER4", "bot message answered in its own turn after the run, receipt returned")
}
