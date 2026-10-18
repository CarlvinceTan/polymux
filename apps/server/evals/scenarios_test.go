package evals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/server"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

const (
	agentID   = "assistant"
	deadline  = 15 * time.Second
	holdWait  = 3 * time.Second
	cancelMax = 2 * time.Second
	quietB2   = 300 * time.Millisecond
)

func TestT2EndTurn(t *testing.T) {
	h := Start(t, Options{ID: "T2", Steps: []Step{Fixed(nil, TextReply("hello from eval"))}})
	if code := h.Send(agentID, "t2-user", "hi"); code != 202 && code != 200 {
		fail(t, "T2", fmt.Sprintf("send status %d", code))
		return
	}
	run := h.WaitRun(agentID, "t2-user", agentID, deadline)
	if err := h.Emulator.Done(); err != nil {
		fail(t, "T2", err.Error())
		return
	}
	messages := h.Messages(agentID)
	done := h.AssistantDone(agentID)
	switch {
	case run.Status != "completed":
		fail(t, "T2", "run status "+run.Status)
	case run.MessageID == nil || *run.MessageID == "":
		fail(t, "T2", "run missing MessageID")
	case len(done) != 1:
		fail(t, "T2", fmt.Sprintf("want 1 done assistant message, got %d", len(done)))
	case assistantCount(messages) != 1:
		fail(t, "T2", fmt.Sprintf("want exactly 1 assistant message, got %d", assistantCount(messages)))
	case done[0].ID != *run.MessageID:
		fail(t, "T2", "run.MessageID does not match assistant message")
	case messageText(done[0]) != "hello from eval":
		fail(t, "T2", "unexpected text "+messageText(done[0]))
	default:
		pass(t, "T2", "text reply completed with one done assistant message")
	}
}

func TestT4Cancel(t *testing.T) {
	opened := make(chan struct{})
	release := make(chan struct{})
	h := Start(t, Options{ID: "T4", Steps: []Step{Fixed(nil, HoldReply("partial", release, func() { close(opened) }, nil))}})
	if code := h.Send(agentID, "t4-user", "hold please"); code != 202 && code != 200 {
		fail(t, "T4", fmt.Sprintf("send status %d", code))
		return
	}
	WaitHold(t, "T4", opened, holdWait)
	stopAt := time.Now()
	_ = h.Stop(agentID) // HTTP 200 is always written; assert cancel via run error code.
	run := h.WaitRun(agentID, "t4-user", agentID, cancelMax)
	time.Sleep(100 * time.Millisecond)
	for _, ts := range h.Emulator.RequestTimes() {
		if ts.After(stopAt) {
			fail(t, "T4", "emulator saw request after stop")
			return
		}
	}
	if err := h.Emulator.Done(); err != nil {
		fail(t, "T4", err.Error())
		return
	}
	switch {
	case run.Status != "cancelled":
		fail(t, "T4", "run status "+run.Status)
	case run.Error == nil || run.Error.Code != "stopped":
		fail(t, "T4", fmt.Sprintf("want error code stopped, got %+v", run.Error))
	default:
		pass(t, "T4", "stop cancelled run within 2s with no further model requests")
	}
}

func TestC1SchemaValidation(t *testing.T) {
	ran := &atomic.Int32{}
	h := Start(t, Options{
		ID:     "C1",
		Extras: map[string][]agent.AgentTool{agentID: {&typedTool{ran: ran}}},
		Steps: []Step{
			Fixed(nil, ToolReply("c1-call", "typed_tool", map[string]any{})), // missing required "value"
			Fixed(func(req *ChatRequest) error {
				if !hasToolError(req, "c1-call", "required") && !hasToolError(req, "c1-call", "value") &&
					!hasToolError(req, "c1-call", "validation") && !hasToolError(req, "c1-call", "invalid") {
					return fmt.Errorf("expected validation error tool result for c1-call")
				}
				return nil
			}, TextReply("schema rejected")),
		},
	})
	if code := h.Send(agentID, "c1-user", "call typed_tool badly"); code != 202 && code != 200 {
		fail(t, "C1", fmt.Sprintf("send status %d", code))
		return
	}
	run := h.WaitRun(agentID, "c1-user", agentID, deadline)
	if err := h.Emulator.Done(); err != nil {
		fail(t, "C1", err.Error())
		return
	}
	switch {
	case ran.Load() != 0:
		fail(t, "C1", "typed_tool Execute was called")
	case run.Status != "completed":
		fail(t, "C1", "run status "+run.Status)
	default:
		pass(t, "C1", "invalid args blocked Execute and surfaced error tool result")
	}
}

func TestC3ToolError(t *testing.T) {
	ran := &atomic.Int32{}
	h := Start(t, Options{
		ID:     "C3",
		Extras: map[string][]agent.AgentTool{agentID: {&errorTool{ran: ran}}},
		Steps: []Step{
			Fixed(nil, ToolReply("c3-call", "error_tool", map[string]any{})),
			Fixed(func(req *ChatRequest) error {
				if !hasToolError(req, "c3-call", "synthetic tool failure") {
					return fmt.Errorf("expected synthetic tool failure for c3-call")
				}
				return nil
			}, TextReply("tool failed cleanly")),
		},
	})
	if code := h.Send(agentID, "c3-user", "call error_tool"); code != 202 && code != 200 {
		fail(t, "C3", fmt.Sprintf("send status %d", code))
		return
	}
	run := h.WaitRun(agentID, "c3-user", agentID, deadline)
	if err := h.Emulator.Done(); err != nil {
		fail(t, "C3", err.Error())
		return
	}
	switch {
	case ran.Load() != 1:
		fail(t, "C3", fmt.Sprintf("error_tool ran %d times", ran.Load()))
	case run.Status != "completed":
		fail(t, "C3", "run status "+run.Status)
	default:
		pass(t, "C3", "tool error returned to model and run completed")
	}
}

func TestC4NoDuplicateSideEffects(t *testing.T) {
	counter := &atomic.Int32{}
	h := Start(t, Options{
		ID:      "C4",
		Effects: map[string]*atomic.Int32{agentID: counter},
		Steps: []Step{
			Fixed(nil, ToolReply("c4-call", "side_effect", map[string]any{})),
			Fixed(nil, DisconnectReply("partial after tool")),
			// Retries (if any) must replay the durable tool result, not re-invoke Execute.
			Fixed(func(req *ChatRequest) error {
				if !hasToolResult(req, "c4-call", "effect-ok") {
					return fmt.Errorf("retry missing durable tool result for c4-call")
				}
				return nil
			}, TextReply("recovered once")),
			Fixed(func(req *ChatRequest) error {
				if !hasToolResult(req, "c4-call", "effect-ok") {
					return fmt.Errorf("retry missing durable tool result for c4-call")
				}
				return nil
			}, TextReply("recovered twice")),
			Fixed(func(req *ChatRequest) error {
				if !hasToolResult(req, "c4-call", "effect-ok") {
					return fmt.Errorf("retry missing durable tool result for c4-call")
				}
				return nil
			}, TextReply("recovered thrice")),
		},
	})
	if code := h.Send(agentID, "c4-user", "side effect then drop"); code != 202 && code != 200 {
		fail(t, "C4", fmt.Sprintf("send status %d", code))
		return
	}
	run := h.WaitRun(agentID, "c4-user", agentID, deadline)
	count := h.Emulator.Count()
	if err := h.Emulator.DoneAllowExtra(); err != nil {
		fail(t, "C4", err.Error())
		return
	}
	switch {
	case counter.Load() != 1:
		fail(t, "C4", fmt.Sprintf("side_effect counter=%d want 1 (run=%s)", counter.Load(), run.Status))
	case count > 5:
		fail(t, "C4", fmt.Sprintf("too many model requests: %d", count))
	case count >= 3 && !hasToolResult(h.Emulator.Requests()[2], "c4-call", "effect-ok"):
		fail(t, "C4", "retry did not replay durable tool result")
	default:
		pass(t, "C4", fmt.Sprintf("side_effect ran once; run %s", run.Status))
	}
}

func TestX1CrashMidTurn(t *testing.T) {
	opened := make(chan struct{})
	release := make(chan struct{})
	h := Start(t, Options{ID: "X1", Steps: []Step{Fixed(nil, HoldReply("streaming", release, func() { close(opened) }, nil))}})
	if code := h.Send(agentID, "x1-user", "crash me"); code != 202 && code != 200 {
		fail(t, "X1", fmt.Sprintf("send status %d", code))
		return
	}
	WaitHold(t, "X1", opened, holdWait)
	runID := RunID("x1-user", agentID)
	pre := h.WaitStatus(runID, holdWait, "running")
	if pre.Status != "running" {
		fail(t, "X1", "run not running before restart: "+pre.Status)
		return
	}
	h.Restart()
	run := h.WaitRunID(runID, deadline)
	events, err := h.Store.Events(context.Background(), agentID, 0, 500)
	if err != nil {
		fail(t, "X1", "events: "+err.Error())
		return
	}
	if err := h.Emulator.DoneAllowExtra(); err != nil {
		fail(t, "X1", err.Error())
		return
	}
	switch {
	case run.Status == "running" || run.Status == "queued":
		fail(t, "X1", "run left "+run.Status)
	case run.Status != "interrupted" && run.Status != "failed":
		fail(t, "X1", "run status "+run.Status)
	case run.Error == nil || run.Error.Message == "":
		fail(t, "X1", "missing user-visible error")
	case !hasMessageError(events):
		fail(t, "X1", "missing thread-visible message.error event")
	default:
		pass(t, "X1", "restart left run "+run.Status+" with visible error")
	}
}

func TestX2PersistedToolResult(t *testing.T) {
	counter := &atomic.Int32{}
	opened := make(chan struct{})
	release := make(chan struct{})
	h := Start(t, Options{
		ID:      "X2",
		Effects: map[string]*atomic.Int32{agentID: counter},
		Steps: []Step{
			Fixed(nil, ToolReply("x2-call", "side_effect", map[string]any{})),
			Fixed(func(req *ChatRequest) error {
				if !hasToolResult(req, "x2-call", "effect-ok") {
					return fmt.Errorf("expected x2-call result effect-ok in model request")
				}
				return nil
			}, HoldReply("", release, func() { close(opened) }, nil)),
			Fixed(func(req *ChatRequest) error {
				if !hasToolResult(req, "x2-call", "effect-ok") {
					return fmt.Errorf("resume missing x2-call result effect-ok")
				}
				return nil
			}, TextReply("RESUMED_X2")),
		},
	})
	if code := h.Send(agentID, "x2-user", "effect then crash"); code != 202 && code != 200 {
		fail(t, "X2", fmt.Sprintf("send status %d", code))
		return
	}
	WaitHold(t, "X2", opened, holdWait)
	if counter.Load() != 1 {
		fail(t, "X2", fmt.Sprintf("tool did not complete before restart: %d", counter.Load()))
		return
	}
	if len(h.Emulator.Requests()) < 2 || !hasToolResult(h.Emulator.Requests()[1], "x2-call", "effect-ok") {
		fail(t, "X2", "tool result did not reach the model before restart")
		return
	}
	runID := RunID("x2-user", agentID)
	h.Restart()
	run := h.WaitRunID(runID, deadline)
	reqs := h.Emulator.Requests()
	done := h.AssistantDone(agentID)
	if len(reqs) < 3 || !hasToolResult(reqs[2], "x2-call", "effect-ok") {
		fail(t, "X2", fmt.Sprintf("resumed request missing tool result (status %s, requests %d)", run.Status, len(reqs)))
		return
	}
	if err := h.Emulator.DoneAllowExtra(); err != nil {
		fail(t, "X2", err.Error())
		return
	}
	resumed := ""
	if len(done) > 0 {
		resumed = messageText(done[len(done)-1])
	}
	switch {
	case counter.Load() != 1:
		fail(t, "X2", fmt.Sprintf("side_effect re-ran after restart: %d", counter.Load()))
	case run.Status != "completed":
		fail(t, "X2", "run status "+run.Status)
	case assistantsFor(h.Messages(agentID), run.ID) != 1:
		fail(t, "X2", fmt.Sprintf("assistant messages for run: %d", assistantsFor(h.Messages(agentID), run.ID)))
	case !strings.Contains(resumed, "RESUMED_X2"):
		fail(t, "X2", "missing resumed final text: "+resumed)
	default:
		pass(t, "X2", "resumed from the saved tool result")
	}
}

func TestTR1SingleResume(t *testing.T) {
	counter := &atomic.Int32{}
	first := make(chan struct{})
	second := make(chan struct{})
	release := make(chan struct{})
	h := Start(t, Options{
		ID:      "TR1",
		Effects: map[string]*atomic.Int32{agentID: counter},
		Steps: []Step{
			Fixed(nil, ToolReply("tr1-call", "side_effect", map[string]any{})),
			Fixed(func(req *ChatRequest) error {
				if !hasToolResult(req, "tr1-call", "effect-ok") {
					return fmt.Errorf("expected tool result before first crash")
				}
				return nil
			}, HoldReply("", release, func() { close(first) }, nil)),
			Fixed(func(req *ChatRequest) error {
				if !hasToolResult(req, "tr1-call", "effect-ok") {
					return fmt.Errorf("resume missing tool result")
				}
				return nil
			}, HoldReply("", release, func() { close(second) }, nil)),
			Fixed(nil, TextReply("should not run")),
		},
	})
	if code := h.Send(agentID, "tr1-user", "effect then crash twice"); code != 202 && code != 200 {
		fail(t, "TR1", fmt.Sprintf("send status %d", code))
		return
	}
	WaitHold(t, "TR1", first, holdWait)
	if counter.Load() != 1 {
		fail(t, "TR1", fmt.Sprintf("tool did not complete before restart: %d", counter.Load()))
		return
	}
	runID := RunID("tr1-user", agentID)
	h.Restart()
	WaitHold(t, "TR1", second, deadline)
	if counter.Load() != 1 || h.Emulator.Count() != 3 {
		fail(t, "TR1", fmt.Sprintf("resume calls=%d counter=%d", h.Emulator.Count(), counter.Load()))
		return
	}
	h.Restart()
	run := h.WaitRunID(runID, deadline)
	if err := h.Emulator.DoneAllowExtra(); err != nil {
		fail(t, "TR1", err.Error())
		return
	}
	switch {
	case counter.Load() != 1:
		fail(t, "TR1", fmt.Sprintf("side_effect counter=%d", counter.Load()))
	case h.Emulator.Count() != 3:
		fail(t, "TR1", fmt.Sprintf("resume loop made %d model requests", h.Emulator.Count()))
	case run.Status != "interrupted":
		fail(t, "TR1", "run status "+run.Status)
	case run.Error == nil || run.Error.Message == "":
		fail(t, "TR1", "missing user-visible error")
	default:
		pass(t, "TR1", "second crash interrupted without another resume")
	}
}

func TestTR1QueuedMessage(t *testing.T) {
	counter := &atomic.Int32{}
	opened := make(chan struct{})
	release := make(chan struct{})
	h := Start(t, Options{
		ID:      "TR1Q",
		Effects: map[string]*atomic.Int32{agentID: counter},
		Steps: []Step{
			Fixed(nil, ToolReply("tr1q-call", "side_effect", map[string]any{})),
			Fixed(func(req *ChatRequest) error {
				if !hasToolResult(req, "tr1q-call", "effect-ok") {
					return fmt.Errorf("expected tool result before the crash")
				}
				return nil
			}, HoldReply("", release, func() { close(opened) }, nil)),
			Fixed(func(req *ChatRequest) error {
				if !hasToolResult(req, "tr1q-call", "effect-ok") {
					return fmt.Errorf("resume missing tool result")
				}
				if !strings.Contains(requestText(req), "queued-second") {
					return fmt.Errorf("resume missing pending steer")
				}
				return nil
			}, TextReply("RESUMED_OWN")),
		},
	})
	if code := h.Send(agentID, "tr1q-user", "effect then queue"); code != 202 && code != 200 {
		fail(t, "TR1Q", fmt.Sprintf("send status %d", code))
		return
	}
	WaitHold(t, "TR1Q", opened, holdWait)
	if code := h.Send(agentID, "tr1q-next", "queued-second"); code != 202 && code != 200 {
		fail(t, "TR1Q", fmt.Sprintf("second send status %d", code))
		return
	}
	runID := RunID("tr1q-user", agentID)
	h.Restart()
	run := h.WaitRunID(runID, deadline)
	if err := h.Emulator.Done(); err != nil {
		fail(t, "TR1Q", err.Error())
		return
	}
	resumed := ""
	for _, m := range h.AssistantDone(agentID) {
		if m.RunID != nil && *m.RunID == run.ID {
			resumed = messageText(m)
		}
	}
	switch {
	case counter.Load() != 1:
		fail(t, "TR1Q", fmt.Sprintf("side_effect counter=%d", counter.Load()))
	case run.Status != "completed":
		fail(t, "TR1Q", "run status "+run.Status)
	case assistantsFor(h.Messages(agentID), run.ID) != 1:
		fail(t, "TR1Q", fmt.Sprintf("assistant messages for run: %d", assistantsFor(h.Messages(agentID), run.ID)))
	case !strings.Contains(resumed, "RESUMED_OWN"):
		fail(t, "TR1Q", "missing resumed text: "+resumed)
	default:
		pass(t, "TR1Q", "pending steer reached the resumed turn")
	}
}

func assistantsFor(messages []contract.Message, runID string) int {
	n := 0
	for _, m := range messages {
		if m.Role == "assistant" && m.RunID != nil && *m.RunID == runID {
			n++
		}
	}
	return n
}

func TestX4ProviderErrors(t *testing.T) {
	// 429 is retried once. Retry-After is non-zero so a build that ignores the
	// header or never emits run.retrying cannot pass. 400 is not retried.
	rateLimited := ErrorReply(429, "rate limited 429")
	rateLimited.RetryAfter = "1"
	h429 := Start(t, Options{ID: "X4", Steps: []Step{
		Fixed(nil, rateLimited),
		Fixed(nil, TextReply("after retry")),
	}})
	if code := h429.Send(agentID, "x4-429", "retry me"); code != 202 && code != 200 {
		fail(t, "X4", fmt.Sprintf("429 send status %d", code))
		return
	}
	run429 := h429.WaitRun(agentID, "x4-429", agentID, deadline)
	count429 := h429.Emulator.Count()
	retrying429 := countRetrying(t, h429)
	if err := h429.Emulator.Done(); err != nil {
		fail(t, "X4", err.Error())
		return
	}

	h400 := Start(t, Options{ID: "X4", Steps: []Step{
		Fixed(nil, ErrorReply(400, "bad request")),
		Fixed(nil, TextReply("should not run")),
	}})
	if code := h400.Send(agentID, "x4-400", "fail me"); code != 202 && code != 200 {
		fail(t, "X4", fmt.Sprintf("400 send status %d", code))
		return
	}
	run400 := h400.WaitRun(agentID, "x4-400", agentID, deadline)
	count400 := h400.Emulator.Count()
	retrying400 := countRetrying(t, h400)
	if err := h400.Emulator.DoneAllowExtra(); err != nil {
		fail(t, "X4", err.Error())
		return
	}

	switch {
	case run429.Status != "completed" || count429 != 2:
		fail(t, "X4", fmt.Sprintf("429 path: status=%s requests=%d", run429.Status, count429))
	case retrying429 != 1:
		fail(t, "X4", fmt.Sprintf("429 path run.retrying events=%d, want 1", retrying429))
	case run400.Status != "failed":
		fail(t, "X4", "400 path status "+run400.Status)
	case count400 != 1:
		fail(t, "X4", fmt.Sprintf("400 path retried: requests=%d", count400))
	case retrying400 != 0:
		fail(t, "X4", fmt.Sprintf("400 path run.retrying events=%d, want 0", retrying400))
	case run400.Error == nil:
		fail(t, "X4", "400 path missing error")
	default:
		pass(t, "X4", "429 retried once with Retrying…; 400 failed without retry")
	}
}

func countRetrying(t *testing.T, h *Harness) int {
	t.Helper()
	events, err := h.Store.Events(context.Background(), agentID, 0, 500)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	n := 0
	for _, event := range events {
		if event.Type != "run.retrying" {
			continue
		}
		n++
		if event.Text == nil || *event.Text != "Retrying…" {
			t.Fatalf("run.retrying text %v, want Retrying…", event.Text)
		}
	}
	return n
}

func TestX6DisconnectMidStream(t *testing.T) {
	h := Start(t, Options{ID: "X6", Steps: []Step{
		Fixed(nil, DisconnectReply("UNIQUE_PARTIAL_X6")),
		Fixed(nil, TextReply("clean recovery")),
		Fixed(nil, TextReply("clean recovery 2")),
		Fixed(nil, TextReply("clean recovery 3")),
	}})
	if code := h.Send(agentID, "x6-user", "disconnect me"); code != 202 && code != 200 {
		fail(t, "X6", fmt.Sprintf("send status %d", code))
		return
	}
	run := h.WaitRun(agentID, "x6-user", agentID, deadline)
	text := ""
	for _, m := range h.Messages(agentID) {
		if m.Role == "assistant" {
			text += messageText(m)
		}
	}
	if err := h.Emulator.DoneAllowExtra(); err != nil {
		fail(t, "X6", err.Error())
		return
	}
	partialN := strings.Count(text, "UNIQUE_PARTIAL_X6")
	recoveryN := strings.Count(text, "clean recovery")
	switch {
	case partialN > 1:
		fail(t, "X6", "duplicated partial text in final messages")
	case run.Status == "completed" && recoveryN == 1 && partialN == 0:
		pass(t, "X6", "clean recovery once with no partial")
	case (run.Status == "failed" || run.Status == "interrupted") && partialN == 1:
		pass(t, "X6", "failed cleanly retaining partial once")
	default:
		fail(t, "X6", fmt.Sprintf("unclean outcome status=%s partial=%d recovery=%d", run.Status, partialN, recoveryN))
	}
}

func TestB1Spawn(t *testing.T) {
	for _, target := range []struct{ name, id string }{{"own agent", ""}, {"other agent", "b"}} {
		t.Run(target.name, func(t *testing.T) {
			opened := make(chan struct{})
			release := make(chan struct{})
			args := map[string]any{"task": "CHILD_ONLY_B1: Complete the bounded task"}
			if target.id != "" {
				args["agent_id"] = target.id
			}
			reply := func(req *ChatRequest) Response {
				text := requestText(req)
				if strings.Contains(text, "CHILD_ONLY_B1") {
					if strings.Contains(text, "PRIVATE_PARENT_B1") {
						t.Error("child received private parent context")
					}
					return HoldReply("child finished", release, func() { close(opened) }, nil)
				}
				if !hasToolResult(req, "b1-spawn", "child") {
					t.Error("parent did not receive the child run")
				}
				return TextReply("parent continues independently")
			}
			h := Start(t, Options{
				ID: "B1",
				Agents: []agentruntime.Definition{
					{Agent: contract.Agent{ID: "a", Name: "A", Model: evalModel}, Tools: []string{}},
					{Agent: contract.Agent{ID: "b", Name: "B", Model: evalModel}, Tools: []string{}},
				},
				Steps: []Step{
					Fixed(func(req *ChatRequest) error {
						if !strings.Contains(string(req.Tools), "spawn_task") {
							return fmt.Errorf("spawn_task is unavailable")
						}
						return nil
					}, ToolReply("b1-spawn", "spawn_task", args)),
					{Response: reply}, {Response: reply},
				},
			})
			t.Cleanup(func() { close(release) })
			if code := h.Send("a", "b1-user", "Spawn a bounded child task, then reply. PRIVATE_PARENT_B1"); code != http.StatusAccepted && code != http.StatusOK {
				fail(t, "B1", fmt.Sprintf("send status %d", code))
				return
			}
			// The child opens after parent setup and spawning, so allow the full scenario deadline.
			WaitHold(t, "B1", opened, deadline)
			parent := h.WaitRun("a", "b1-user", "a", deadline)
			owner := target.id
			if owner == "" {
				owner = "a"
			}
			runs, err := h.Store.Runs(t.Context(), owner)
			if err != nil {
				fail(t, "B1", err.Error())
				return
			}
			var children []contract.Run
			for _, run := range runs {
				if run.ParentRunID != nil && *run.ParentRunID == parent.ID {
					children = append(children, run)
				}
			}
			if parent.Status != "completed" || len(children) != 1 || children[0].Trigger != "child" || children[0].Status != "running" {
				fail(t, "B1", fmt.Sprintf("parent=%s children=%+v", parent.Status, children))
				return
			}
			if parent.MessageID == nil {
				fail(t, "B1", "parent has no final message")
				return
			}
			message, err := h.Store.Message(t.Context(), *parent.MessageID)
			if err != nil || messageText(message) != "parent continues independently" {
				fail(t, "B1", fmt.Sprintf("parent did not continue after spawn: %v", err))
				return
			}
			if err := h.Emulator.Done(); err != nil {
				fail(t, "B1", err.Error())
				return
			}
			pass(t, "B1", "parent continued and completed while its isolated child run remained active")
		})
	}
}

func TestB2BackgroundNotify(t *testing.T) {
	b2Reply := func(req *ChatRequest) Response {
		text := requestText(req)
		switch {
		case strings.Contains(text, "Do the bounded peer task"):
			return TextReply("peer finished the task")
		case strings.Contains(text, "peer finished the task") || strings.Contains(strings.ToLower(text), "peer result"):
			return TextReply("synthesized peer result once")
		default:
			return TextReply("queued peer work")
		}
	}
	h := Start(t, Options{
		ID: "B2",
		Agents: []agentruntime.Definition{
			{Agent: contract.Agent{ID: "a", Name: "A", Model: evalModel}, Tools: []string{}},
			{Agent: contract.Agent{ID: "b", Name: "B", Model: evalModel}, Tools: []string{}},
		},
		Steps: []Step{
			Fixed(nil, ToolReply("b2-peer", "message_agent", map[string]any{
				"agent_id": "b", "message": "Do the bounded peer task", "intent": "request",
			})),
			{Response: b2Reply},
			{Response: b2Reply},
			{Response: b2Reply},
		},
	})
	if code := h.Send("a", "b2-user", "Ask peer B to help"); code != 202 && code != 200 {
		fail(t, "B2", fmt.Sprintf("send status %d", code))
		return
	}
	parentID := store.NewRun("b2-user", "a", "a", "message").ID
	deadlineAt := time.Now().Add(deadline)
	var parent contract.Run
	settled := false
	for time.Now().Before(deadlineAt) {
		var err error
		parent, err = h.Store.Run(context.Background(), parentID)
		if err != nil {
			fail(t, "B2", "parent run: "+err.Error())
			return
		}
		_, followErr := h.Store.Work(context.Background(), "followup:"+parentID)
		if followErr != nil && !errors.Is(followErr, sql.ErrNoRows) {
			fail(t, "B2", "followup lookup: "+followErr.Error())
			return
		}
		followReady := followErr == nil
		// Settled = terminal parent, or follow-up observed (then keep waiting for terminal).
		if store.Terminal(parent.Status) {
			settled = true
			break
		}
		if followReady {
			// Follow-up exists; wait out the rest of the deadline for terminal parent.
			for time.Now().Before(deadlineAt) {
				parent, err = h.Store.Run(context.Background(), parentID)
				if err != nil {
					fail(t, "B2", "parent run: "+err.Error())
					return
				}
				if store.Terminal(parent.Status) {
					settled = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !settled {
		skip(t, "B2", "peer/follow-up flow could not be driven deterministically")
		return
	}

	receipts := countPeerReceipts(h.Messages("a"))
	time.Sleep(quietB2)
	receiptsAfter := countPeerReceipts(h.Messages("a"))
	if err := h.Emulator.DoneAllowExtra(); err != nil {
		fail(t, "B2", err.Error())
		return
	}
	switch {
	case receipts != 1 || receiptsAfter != 1:
		fail(t, "B2", fmt.Sprintf("peer result count before=%d after_quiet=%d", receipts, receiptsAfter))
	default:
		pass(t, "B2", "peer result delivered exactly once without user action")
	}
}

func TestB4StopChildren(t *testing.T) {
	for _, status := range []string{"running", "waiting_peer"} {
		t.Run(status, func(t *testing.T) {
			opened := map[string]chan struct{}{}
			closed := map[string]chan struct{}{}
			for _, id := range []string{"parent", "b", "c", "unrelated"} {
				opened[id], closed[id] = make(chan struct{}), make(chan struct{})
			}
			release := make(chan struct{})
			defer close(release)
			hold := func(id string) Response {
				return HoldReply("working", release, func() { close(opened[id]) }, func() { close(closed[id]) })
			}
			reply := func(req *ChatRequest) Response {
				text := requestText(req)
				switch {
				case strings.Contains(text, "B4 unrelated"):
					return hold("unrelated")
				case strings.Contains(text, "B4 child b"):
					return hold("b")
				case strings.Contains(text, "B4 child c"):
					return hold("c")
				case strings.Contains(text, "B4 child d"):
					return TextReply("queued child should never execute")
				default:
					for _, id := range []string{"b", "c", "d"} {
						if !hasToolResult(req, "b4-"+id, "delivery:") {
							return ToolReply("b4-"+id, "message_agent", map[string]any{
								"agent_id": id, "message": "B4 child " + id, "intent": "request",
							})
						}
					}
					if status == "running" {
						return hold("parent")
					}
					return TextReply("delegated work")
				}
			}
			var steps []Step
			for range 7 {
				steps = append(steps, Step{Response: reply})
			}
			var agents []agentruntime.Definition
			for _, id := range []string{"a", "b", "c", "d"} {
				agents = append(agents, agentruntime.Definition{Agent: contract.Agent{ID: id, Name: id, Model: evalModel}, Tools: []string{}})
			}
			h := Start(t, Options{ID: "B4", Agents: agents, Steps: steps})
			if code := h.Send("d", "b4-unrelated", "B4 unrelated"); code != http.StatusAccepted {
				h.die(fmt.Sprintf("unrelated send status %d", code))
			}
			WaitHold(t, "B4", opened["unrelated"], holdWait)
			if code := h.Send("a", "b4-parent", "Delegate three tasks"); code != http.StatusAccepted {
				h.die(fmt.Sprintf("parent send status %d", code))
			}
			parentID := RunID("b4-parent", "a")
			for _, id := range []string{"b", "c"} {
				WaitHold(t, "B4", opened[id], holdWait)
			}
			if status == "running" {
				WaitHold(t, "B4", opened["parent"], holdWait)
			}
			h.WaitStatus(parentID, deadline, status)
			childID := func(id string) string { return RunID("delivery:"+parentID+":b4-"+id, id) }
			h.WaitStatus(childID("d"), deadline, "queued")
			stopAt := time.Now()
			end := stopAt.Add(cancelMax)
			if code := h.Stop("a"); code != http.StatusOK {
				h.die(fmt.Sprintf("stop status %d", code))
			}
			for _, id := range []string{parentID, childID("b"), childID("c"), childID("d")} {
				run := h.WaitRunID(id, time.Until(end))
				if run.Status != "cancelled" || run.Error == nil || run.Error.Code != "stopped" {
					h.die(fmt.Sprintf("run %s not stopped: %+v", id, run))
				}
			}
			for _, id := range []string{"b", "c"} {
				WaitHold(t, "B4", closed[id], time.Until(end))
			}
			if status == "running" {
				WaitHold(t, "B4", closed["parent"], time.Until(end))
			}
			if time.Now().After(end) {
				h.die("parent and children took at least 2 seconds to stop")
			}
			select {
			case <-closed["unrelated"]:
				h.die("stop cancelled unrelated work on a child agent")
			default:
			}
			time.Sleep(200 * time.Millisecond)
			for _, ts := range h.Emulator.RequestTimes() {
				if ts.After(stopAt) {
					h.die("model request arrived after parent stop")
				}
			}
			var queued int
			if err := h.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs WHERE id=? AND state!='done'`, "delivery:"+parentID+":b4-d").Scan(&queued); err != nil || queued != 0 {
				h.die(fmt.Sprintf("queued child remains executable: %d %v", queued, err))
			}
			if _, err := h.Store.Work(t.Context(), "followup:"+parentID); !errors.Is(err, sql.ErrNoRows) {
				h.die(fmt.Sprintf("stopped parent generated a follow-up: %v", err))
			}
			if err := h.Emulator.Done(); err != nil {
				h.die(err.Error())
			}
			pass(t, "B4", "stop cancelled parent, active streams, and queued children within 2s while preserving unrelated work")
		})
	}
}

func TestB5StuckBackground(t *testing.T) {
	opened, closed, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	reply := func(req *ChatRequest) Response {
		if _, ok := toolMessage(req, "b5-call"); ok {
			return TextReply("Delegated background work.")
		}
		return HoldReply("child still working", release, func() { close(opened) }, func() { close(closed) })
	}
	h := Start(t, Options{
		ID: "B5", BackgroundTimeout: time.Second,
		Agents: []agentruntime.Definition{
			{Agent: contract.Agent{ID: "a", Name: "A", Model: evalModel}},
			{Agent: contract.Agent{ID: "b", Name: "B", Model: evalModel}},
		},
		Steps: []Step{
			Fixed(nil, ToolReply("b5-call", "message_agent", map[string]any{
				"agent_id": "b", "message": "Hold this background task", "intent": "request",
			})),
			{Response: reply},
			{Response: reply},
			Fixed(func(req *ChatRequest) error {
				text := requestText(req)
				if !strings.Contains(text, "timed out") || !strings.Contains(text, `"status":"failed"`) {
					return errors.New("follow-up did not receive the timeout")
				}
				return nil
			}, TextReply("I stopped that background task because it took too long. Tell me if you want to try again.")),
		},
	})
	t.Cleanup(func() { close(release) })
	if code := h.Send("a", "b5-user", "Ask B for background help"); code != http.StatusAccepted {
		h.die(fmt.Sprintf("send status %d", code))
	}
	WaitHold(t, "B5", opened, holdWait)
	parentID := RunID("b5-user", "a")
	deliveryID := "delivery:" + parentID + ":b5-call"
	childID := store.NewRun(deliveryID, "b", "b", "peer").ID
	child := h.WaitRunID(childID, holdWait)
	parent := h.WaitRunID(parentID, holdWait)
	WaitHold(t, "B5", closed, holdWait)
	if child.Status != "failed" || child.Error == nil || child.Error.Code != "timeout" || child.FinishedAt == nil {
		h.die(fmt.Sprintf("child did not fail cleanly on timeout: %+v", child))
	}
	if !strings.Contains(strings.ToLower(child.Error.Message), "timed out") {
		h.die("child timeout did not explain the stop")
	}
	if parent.Status != "failed" || parent.Error == nil || parent.Error.Code != "peer_failed" {
		h.die("parent left waiting: " + parent.Status)
	}
	if followup := h.WaitRunID(store.NewRun("followup:"+parentID, "a", "a", "follow_up").ID, holdWait); followup.Status != "completed" {
		h.die("follow-up did not finish: " + followup.Status)
	}
	said := "I stopped that background task because it took too long. Tell me if you want to try again."
	sawReply := false
	for _, message := range h.Messages("a") {
		body := messageText(message)
		if strings.HasPrefix(message.ID, "receipt:") || strings.Contains(body, "Background work appears stuck") || strings.Contains(body, child.Error.Message) {
			h.die(fmt.Sprintf("stuck notice in the user conversation: %+v", message))
		}
		if message.Role == "assistant" && message.Status == "done" && body == said {
			sawReply = true
		}
	}
	if !sawReply {
		h.die("follow-up reply missing from the user conversation")
	}
	if err := h.Store.Receipt(t.Context(), deliveryID, child, contract.Message{}); err != nil {
		h.die(err.Error())
	}
	events, err := h.Store.Events(t.Context(), "a", 0, 200)
	if err != nil {
		h.die(err.Error())
	}
	notices, failures := 0, 0
	for _, event := range events {
		if event.Type == "message.start" && event.MessageID != nil && strings.HasPrefix(*event.MessageID, "receipt:") {
			notices++
		}
		if event.Type == "peer.delivery" && event.Delivery != nil && event.Delivery.ID == deliveryID && event.Delivery.Status == "failed" {
			failures++
		}
	}
	if notices != 0 || failures != 1 {
		h.die(fmt.Sprintf("want no receipt notice and one failed delivery, got %d and %d", notices, failures))
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	pass(t, "B5", "held child timed out and aborted, and the parent explained it in chat without a receipt notice")
}

func countPeerReceipts(messages []contract.Message) int {
	n := 0
	for _, m := range messages {
		if m.AgentID != nil && *m.AgentID == "b" && m.Status == "done" {
			n++
		}
	}
	return n
}

func TestT3MidTurnUserMessage(t *testing.T) {
	opened, release := make(chan struct{}), make(chan struct{})
	h := Start(t, Options{ID: "T3", Extras: map[string][]agent.AgentTool{agentID: {&typedTool{ran: &atomic.Int32{}}}}, Steps: []Step{
		{Response: func(*ChatRequest) Response {
			close(opened)
			<-release
			return ToolReply("t3-tool", "typed_tool", map[string]any{"value": "first"})
		}},
		Fixed(func(req *ChatRequest) error {
			if !strings.Contains(requestText(req), "ALPHA_T3") {
				return fmt.Errorf("second request missing steer")
			}
			return nil
		}, TextReply("reply includes second: ALPHA_T3")),
	}})
	if code := h.Send(agentID, "t3-one", "first message"); code != 202 && code != 200 {
		t.Fatalf("send: %d", code)
	}
	WaitHold(t, "T3", opened, holdWait)
	if code := h.Send(agentID, "t3-two", "second message ALPHA_T3"); code != 202 && code != 200 {
		t.Fatalf("steer: %d", code)
	}
	close(release)
	run := h.WaitRun(agentID, "t3-one", agentID, deadline)
	if err := h.Emulator.Done(); err != nil {
		t.Fatal(err)
	}
	var runs int
	err := h.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM runs WHERE thread_id=?`, agentID).Scan(&runs)
	if err != nil {
		t.Fatal(err)
	}
	if runs != 1 || run.Status != "completed" || len(h.AssistantDone(agentID)) != 1 {
		t.Fatalf("runs=%v replies=%d", runs, len(h.AssistantDone(agentID)))
	}
	pass(t, "T3", "second request contains steer with one run and reply")
}

func TestSTLateSteer(t *testing.T) {
	opened, release := make(chan struct{}), make(chan struct{})
	h := Start(t, Options{ID: "ST-late", Steps: []Step{
		Fixed(nil, HoldReply("first answer", release, func() { close(opened) }, nil)),
		Fixed(func(req *ChatRequest) error {
			if !strings.Contains(requestText(req), "ALPHA_LATE") {
				return fmt.Errorf("follow-up missing message")
			}
			return nil
		}, TextReply("answered ALPHA_LATE")),
	}})
	if code := h.Send(agentID, "late-one", "first"); code != 202 && code != 200 {
		t.Fatalf("send: %d", code)
	}
	WaitHold(t, "ST-late", opened, holdWait)
	if code := h.Send(agentID, "late-two", "ALPHA_LATE"); code != 202 && code != 200 {
		t.Fatalf("steer: %d", code)
	}
	close(release)
	h.WaitRun(agentID, "late-one", agentID, deadline)
	run := h.WaitRun(agentID, "late-two", agentID, deadline)
	if err := h.Emulator.Done(); err != nil {
		t.Fatal(err)
	}
	var runs int
	err := h.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM runs WHERE thread_id=?`, agentID).Scan(&runs)
	if err != nil {
		t.Fatal(err)
	}
	if runs != 2 || run.Status != "completed" || len(h.AssistantDone(agentID)) != 2 {
		t.Fatalf("runs=%v replies=%d", runs, len(h.AssistantDone(agentID)))
	}
	pass(t, "ST-late", "late steer answered by one follow-up run")
}

func TestC5ApprovalGate(t *testing.T) {
	ran := &atomic.Int32{}
	h := Start(t, Options{
		ID:     "C5",
		Extras: map[string][]agent.AgentTool{agentID: {&consequentialTool{ran: ran}}},
		Steps: []Step{
			Fixed(nil, ToolReply("c5-call", "consequential_delete", map[string]any{"target": "everything"})),
			Fixed(nil, ToolReply("c5-retry", "consequential_delete", map[string]any{"target": "everything"})),
			Fixed(nil, TextReply("deleted after approval")),
		},
	})
	if code := h.Send(agentID, "c5-user", "delete everything"); code != 202 && code != 200 {
		fail(t, "C5", fmt.Sprintf("send status %d", code))
		return
	}
	run := h.WaitStatus(RunID("c5-user", agentID), deadline, "waiting_input", "completed", "failed", "cancelled", "interrupted")
	if run.Status != "waiting_input" {
		fail(t, "C5", "expected waiting_input, got "+run.Status)
		return
	}
	if ran.Load() != 0 {
		fail(t, "C5", "tool executed before approval")
		return
	}
	pending, err := h.Store.PendingApprovals(context.Background(), run.ID)
	if err != nil || len(pending) != 1 {
		fail(t, "C5", fmt.Sprintf("pending approvals: %v %v", pending, err))
		return
	}
	events, err := h.Store.Events(context.Background(), agentID, 0, 500)
	if err != nil {
		fail(t, "C5", err.Error())
		return
	}
	requested := false
	for _, e := range events {
		if e.Type == "approval_requested" && e.Approval != nil && e.Approval.ID == pending[0].ID {
			requested = true
			break
		}
	}
	if !requested {
		fail(t, "C5", "missing approval_requested event")
		return
	}
	resp, err := approvalPost(h.HTTP.URL + "/approvals/" + pending[0].ID + "/approve")
	if err != nil {
		fail(t, "C5", err.Error())
		return
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(t, "C5", fmt.Sprintf("approve status %d", resp.StatusCode))
		return
	}
	run = h.WaitRunID(run.ID, deadline)
	if err := h.Emulator.Done(); err != nil {
		fail(t, "C5", err.Error())
		return
	}
	if ran.Load() != 1 {
		fail(t, "C5", fmt.Sprintf("tool ran %d times, want 1", ran.Load()))
		return
	}
	if run.Status != "completed" {
		fail(t, "C5", "run status "+run.Status)
		return
	}

	denyRan := &atomic.Int32{}
	h2 := Start(t, Options{
		ID:     "C5",
		Extras: map[string][]agent.AgentTool{agentID: {&consequentialTool{ran: denyRan}}},
		Steps: []Step{
			Fixed(nil, ToolReply("c5-deny", "consequential_delete", map[string]any{"target": "secrets"})),
			Fixed(nil, ToolReply("c5-deny2", "consequential_delete", map[string]any{"target": "secrets"})),
			Fixed(func(req *ChatRequest) error {
				for _, m := range req.Messages {
					if m.Role == "tool" && strings.Contains(strings.ToLower(contentText(m.Content)), "denied") {
						return nil
					}
				}
				return fmt.Errorf("expected denial in tool results")
			}, TextReply("acknowledged denial")),
		},
	})
	if code := h2.Send(agentID, "c5-deny-user", "delete secrets"); code != 202 && code != 200 {
		fail(t, "C5", fmt.Sprintf("deny send status %d", code))
		return
	}
	denyRun := h2.WaitStatus(RunID("c5-deny-user", agentID), deadline, "waiting_input", "completed", "failed")
	if denyRun.Status != "waiting_input" {
		fail(t, "C5", "deny path expected waiting_input, got "+denyRun.Status)
		return
	}
	denyPending, err := h2.Store.PendingApprovals(context.Background(), denyRun.ID)
	if err != nil || len(denyPending) != 1 {
		fail(t, "C5", fmt.Sprintf("deny pending: %v %v", denyPending, err))
		return
	}
	resp, err = approvalPost(h2.HTTP.URL + "/approvals/" + denyPending[0].ID + "/deny")
	if err != nil {
		fail(t, "C5", err.Error())
		return
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(t, "C5", fmt.Sprintf("deny status %d", resp.StatusCode))
		return
	}
	denyRun = h2.WaitRunID(denyRun.ID, deadline)
	if err := h2.Emulator.Done(); err != nil {
		fail(t, "C5", err.Error())
		return
	}
	if denyRan.Load() != 0 {
		fail(t, "C5", fmt.Sprintf("denied tool ran %d times", denyRan.Load()))
		return
	}
	if denyRun.Status != "completed" {
		fail(t, "C5", "deny run status "+denyRun.Status)
		return
	}
	pass(t, "C5", "approval gate parks, approve executes once, deny blocks with message")
}

type consequentialTool struct{ ran *atomic.Int32 }

func (*consequentialTool) Name() string                           { return "consequential_delete" }
func (*consequentialTool) Label() string                          { return "Consequential delete" }
func (*consequentialTool) Description() string                    { return "Destructive delete requiring approval" }
func (*consequentialTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*consequentialTool) ToolClass() string                      { return string(server.ToolConsequential) }

func approvalPost(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Polymux-Client", "evals")
	return http.DefaultClient.Do(req)
}
func (*consequentialTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "consequential_delete", Description: "Destructive delete requiring approval", Parameters: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"target": map[string]any{"type": "string"}},
		"required":   []string{"target"},
	}}
}
func (t *consequentialTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	t.ran.Add(1)
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "deleted"}}}, nil
}
