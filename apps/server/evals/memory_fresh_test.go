package evals

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

const privateMemory = "Durable memory for this agent and conversation only"

// latestUser is the text of the request's last user message, where turn context goes.
func latestUser(req *ChatRequest) (int, string) {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return i, contentText(req.Messages[i].Content)
		}
	}
	return -1, ""
}

// staleMemory reports an earlier user message that still carries a memory block.
func staleMemory(req *ChatRequest) bool {
	last, _ := latestUser(req)
	for i, m := range req.Messages[:max(last, 0)] {
		if m.Role == "user" && strings.Contains(contentText(m.Content), privateMemory) && i != last {
			return true
		}
	}
	return false
}

func TestMEM2CorrectionIsFresh(t *testing.T) {
	const before, after = "Prefers morning meetings.", "Prefers afternoon meetings."
	revision := regexp.MustCompile(`"name":"meeting-time","text":"` + regexp.QuoteMeta(before) + `","revision":"([^"]+)"`)
	var corrected *ChatRequest
	h := Start(t, Options{ID: "MEM2", Steps: []Step{
		Fixed(nil, ToolReply("mem2-save", "save_memory", map[string]any{"name": "meeting-time", "text": before, "revision": ""})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "mem2-save", `"revision"`) {
				return fmt.Errorf("first save failed")
			}
			return nil
		}, TextReply("Saved.")),
		{Expect: func(req *ChatRequest) error {
			if _, text := latestUser(req); !revision.MatchString(text) {
				return fmt.Errorf("the correction turn lacks the saved fact and its revision")
			}
			corrected = req
			return nil
		}, Response: func(req *ChatRequest) Response {
			_, text := latestUser(req)
			match := revision.FindStringSubmatch(text)
			if match == nil {
				return TextReply("missing revision")
			}
			return ToolReply("mem2-correct", "save_memory", map[string]any{"name": "meeting-time", "text": after, "revision": match[1]})
		}},
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "mem2-correct", after) {
				return fmt.Errorf("the correction did not save")
			}
			if _, text := latestUser(req); !strings.Contains(text, after) || strings.Contains(text, before) {
				return fmt.Errorf("the next request in the same turn still shows the old fact")
			}
			return nil
		}, TextReply("Updated.")),
		Fixed(func(req *ChatRequest) error {
			if _, text := latestUser(req); !strings.Contains(text, after) || strings.Contains(text, before) {
				return fmt.Errorf("the next turn's memory lacks the correction")
			}
			if staleMemory(req) {
				return fmt.Errorf("an earlier message still carries an old memory block")
			}
			// The system prompt and older history stay byte-identical, so provider caching still holds.
			if last, _ := latestUser(corrected); corrected == nil || len(req.Messages) < last || !reflect.DeepEqual(req.Messages[:last], corrected.Messages[:last]) {
				return fmt.Errorf("history before the previous turn changed between requests")
			}
			return nil
		}, TextReply("Afternoons.")),
		Fixed(func(req *ChatRequest) error {
			if _, text := latestUser(req); !strings.Contains(text, after) || strings.Contains(text, before) {
				return fmt.Errorf("the correction did not survive a restart")
			}
			return nil
		}, TextReply("Still afternoons.")),
	}})
	for _, turn := range []struct{ id, text string }{
		{"mem2-first", "Remember that I prefer morning meetings."},
		{"mem2-fix", "Correction: I prefer afternoon meetings now."},
		{"mem2-ask", "When do I prefer meetings?"},
		{"mem2-again", "Remind me when I prefer meetings."},
	} {
		if turn.id == "mem2-again" {
			h.Restart()
		}
		if code := h.Send(agentID, turn.id, turn.text); code != http.StatusAccepted {
			h.die(fmt.Sprintf("%s status %d", turn.id, code))
		}
		if run := h.WaitRun(agentID, turn.id, agentID, deadline); run.Status != "completed" {
			h.die(turn.id + " run status " + run.Status)
		}
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	pass(t, "MEM2", "a corrected fact reaches the same turn's next request, the next turn and a restart, with no stale memory left in history")
}

func TestMEM3UnreadableMemoryFailsOnce(t *testing.T) {
	h := Start(t, Options{ID: "MEM3"})
	// A file where the agent's memory folder belongs makes every memory read fail.
	if err := os.MkdirAll(filepath.Join(h.Dir, "memory"), 0700); err != nil {
		h.die(err.Error())
	}
	if err := os.WriteFile(filepath.Join(h.Dir, "memory", agentID), nil, 0600); err != nil {
		h.die(err.Error())
	}
	if code := h.Send(agentID, "mem3-user", "What do you remember about me?"); code != http.StatusAccepted {
		h.die(fmt.Sprintf("status %d", code))
	}
	run := h.WaitStatus(RunID("mem3-user", agentID), deadline, "failed", "completed")
	if run.Status != "failed" {
		h.die("a turn without its memory ran as " + run.Status)
	}
	time.Sleep(2 * time.Second)
	if count := h.Emulator.Count(); count != 0 {
		h.die(fmt.Sprintf("the model was asked %d times without memory", count))
	}
	if again := h.WaitStatus(run.ID, deadline, "failed", "completed"); again.Status != "failed" || again.Attempt != run.Attempt {
		h.die("the failed turn was retried")
	}
	pass(t, "MEM3", "an unreadable memory store fails the turn once instead of answering without memory or retrying")
}

func TestMEM4MemoryAfterResume(t *testing.T) {
	const fact = "Prefers tea over coffee."
	opened, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	withMemory := func(req *ChatRequest) error {
		if _, text := latestUser(req); !strings.Contains(text, fact) {
			return fmt.Errorf("the request lacks the saved memory")
		}
		return nil
	}
	h := Start(t, Options{ID: "MEM4", Steps: []Step{
		Fixed(nil, ToolReply("mem4-save", "save_memory", map[string]any{"name": "drink", "text": fact, "revision": ""})),
		Fixed(nil, TextReply("Saved.")),
		Fixed(withMemory, HoldReply("Let me th", release, func() { close(opened) }, nil)),
		// A turn resumed after a restart rebuilds its memory too, rather than sending none.
		Fixed(withMemory, TextReply("Tea.")),
	}})
	if code := h.Send(agentID, "mem4-first", "Remember that I prefer tea over coffee."); code != http.StatusAccepted {
		h.die(fmt.Sprintf("status %d", code))
	}
	if run := h.WaitRun(agentID, "mem4-first", agentID, deadline); run.Status != "completed" {
		h.die("first run status " + run.Status)
	}
	if code := h.Send(agentID, "mem4-ask", "What do I like to drink?"); code != http.StatusAccepted {
		h.die(fmt.Sprintf("status %d", code))
	}
	WaitHold(t, "MEM4", opened, holdWait)
	h.Restart()
	if run := h.WaitRun(agentID, "mem4-ask", agentID, deadline); run.Status != "completed" {
		h.die("resumed run status " + run.Status)
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	pass(t, "MEM4", "a turn resumed after a restart still sends its memory")
}
