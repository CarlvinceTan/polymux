package evals

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
)

// A task works in its own thread while the main chat stays free to answer the owner.
func TestTASK1MainChatFreeDuringTask(t *testing.T) {
	const assistant = "assistant"
	working, release := make(chan struct{}), make(chan struct{})
	var started atomic.Bool
	reply := func(req *ChatRequest) Response {
		_, latest := latestUser(req)
		switch {
		case strings.Contains(latest, "This thread is one task"):
			if started.CompareAndSwap(false, true) {
				close(working)
				await(release)
			}
			return TextReply("Task finished.")
		case strings.Contains(latest, "2+2"):
			return TextReply("4")
		case hasToolResult(req, "task1-create", ""):
			return TextReply("Started it in its own thread.")
		default:
			return ToolReply("task1-create", "create_task", map[string]any{"name": "Laptop research", "description": "Compare three travel laptops."})
		}
	}
	steps := make([]Step, 8)
	for n := range steps {
		steps[n] = Step{Response: reply}
	}
	h := Start(t, Options{ID: "TASK1", Steps: steps})
	if code := h.Send(assistant, "task1-user", "research travel laptops as a task"); code != 200 && code != 202 {
		t.Fatalf("send status %d", code)
	}
	WaitHold(t, "TASK1", working, holdWait)
	if code := h.Send(assistant, "task1-quick", "quick one: what's 2+2?"); code != 200 && code != 202 {
		t.Fatalf("send status %d", code)
	}
	answered := false
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end) && !answered; time.Sleep(20 * time.Millisecond) {
		for _, m := range h.AssistantDone(assistant) {
			answered = answered || messageText(m) == "4"
		}
	}
	var busy int
	if err := h.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM runs WHERE thread_id!=? AND status='running'`, assistant).Scan(&busy); err != nil {
		t.Fatal(err)
	}
	close(release)
	if !answered || busy != 1 {
		fail(t, "TASK1", "the main chat waited for the task thread")
		return
	}
	if err := h.Emulator.DoneAllowExtra(); err != nil {
		fail(t, "TASK1", err.Error())
		return
	}
	pass(t, "TASK1", "the main chat answered while the task worked in its own thread")
}

// What the bot writes beside a tool call, such as an answer given mid-run, stays in the chat ahead of its final words.
func TestBEAT1TextBesideWorkStays(t *testing.T) {
	const assistant = "assistant"
	work := ToolReply("beat1-tool", "typed_tool", map[string]any{"value": "first"})
	work.AssistantText = "Roughly 8 km."
	h := Start(t, Options{ID: "BEAT1", Extras: map[string][]agent.AgentTool{assistant: {&typedTool{ran: &atomic.Int32{}}}}, Steps: []Step{
		Fixed(func(*ChatRequest) error { return nil }, work),
		Fixed(func(*ChatRequest) error { return nil }, TextReply("Here are the laptops.")),
	}})
	if code := h.Send(assistant, "beat1-user", "compare laptops, and how far is 5 miles?"); code != 200 && code != 202 {
		t.Fatalf("send status %d", code)
	}
	h.WaitRun(assistant, "beat1-user", "", deadline)
	// Each text part is a bubble in the chat.
	var got []string
	for _, m := range h.AssistantDone(assistant) {
		for _, part := range m.Parts {
			if part.Text != nil {
				got = append(got, *part.Text)
			}
		}
	}
	if strings.Join(got, "|") != "Roughly 8 km.|Here are the laptops." {
		fail(t, "BEAT1", "chat reads "+strings.Join(got, "|"))
		return
	}
	pass(t, "BEAT1", "text beside a tool call stayed in the chat, before the final reply")
}

// A bot that speaks through send_to_user keeps the rest of its text private.
func TestBEAT2NarrationStaysPrivate(t *testing.T) {
	const assistant = "assistant"
	work := ToolReply("beat2-tool", "typed_tool", map[string]any{"value": "first"})
	work.AssistantText = "Let me look that up."
	h := Start(t, Options{ID: "BEAT2", Extras: map[string][]agent.AgentTool{assistant: {&typedTool{ran: &atomic.Int32{}}}}, Steps: []Step{
		Fixed(func(*ChatRequest) error { return nil }, work),
		Fixed(func(*ChatRequest) error { return nil }, ToolReply("beat2-say", "send_to_user", map[string]any{"text": "It's 8 km."})),
		Fixed(func(*ChatRequest) error { return nil }, TextReply("NO_RESPONSE")),
	}})
	if code := h.Send(assistant, "beat2-user", "how far is 5 miles?"); code != 200 && code != 202 {
		t.Fatalf("send status %d", code)
	}
	h.WaitRun(assistant, "beat2-user", "", deadline)
	var got []string
	for _, m := range h.AssistantDone(assistant) {
		got = append(got, messageText(m))
	}
	if strings.Join(got, "|") != "It's 8 km." {
		fail(t, "BEAT2", "chat reads "+strings.Join(got, "|"))
		return
	}
	pass(t, "BEAT2", "only the send_to_user message reached the chat")
}
