package evals

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
)

func TestT5Compaction(t *testing.T) {
	const turns = 200
	const summary = "T5_SUMMARY: The user is working through a long history."
	history := strings.Repeat("history detail ", 100)
	if turns*len(history) <= agentruntime.MaxRequestBytes {
		fail(t, "T5", "fixture history does not exceed the request limit")
		return
	}
	steps := make([]Step, turns*2)
	for i := range steps {
		steps[i] = Step{Expect: func(req *ChatRequest) error {
			if req.Bytes <= 0 || req.Bytes > agentruntime.MaxRequestBytes {
				return fmt.Errorf("request bytes %d exceed limit %d", req.Bytes, agentruntime.MaxRequestBytes)
			}
			if strings.Contains(requestText(req), "You are a context summarization assistant") &&
				!strings.Contains(requestText(req), "history detail") {
				return fmt.Errorf("compaction request is missing the conversation history")
			}
			return nil
		}, Response: func(req *ChatRequest) Response {
			if strings.Contains(requestText(req), "You are a context summarization assistant") {
				return TextReply(summary)
			}
			return TextReply("history reply")
		}}
	}
	h := Start(t, Options{ID: "T5", Steps: steps})
	for i := 0; i < turns; i++ {
		id := fmt.Sprintf("t5-%03d", i)
		text := id + " " + history
		if code := h.Send(agentID, id, text); code != 202 && code != 200 {
			fail(t, "T5", fmt.Sprintf("turn %d send status %d", i, code))
			return
		}
		if run := h.WaitRun(agentID, id, agentID, deadline); run.Status != "completed" {
			fail(t, "T5", fmt.Sprintf("turn %d run status %s: %+v", i, run.Status, run.Error))
			return
		}
	}
	if err := h.Emulator.DoneAllowExtra(); err != nil {
		fail(t, "T5", err.Error())
		return
	}
	requests := h.Emulator.Requests()
	if len(requests) <= turns || !strings.Contains(requestText(requests[len(requests)-1]), summary) ||
		!strings.Contains(requestText(requests[len(requests)-1]), "t5-199") ||
		strings.Contains(requestText(requests[len(requests)-1]), "t5-000") {
		fail(t, "T5", "history was not summarized while retaining the latest turn")
		return
	}
	events, err := h.Store.Events(context.Background(), agentID, 0, 10000)
	if err != nil {
		fail(t, "T5", err.Error())
		return
	}
	messages, err := h.Store.Messages(context.Background(), agentID, 0, turns*2+10)
	if err != nil {
		fail(t, "T5", err.Error())
		return
	}
	users, replies := 0, 0
	for _, message := range messages {
		if message.Role == "user" {
			users++
		}
		if message.Role == "assistant" && message.Status == "done" {
			replies++
		}
	}
	if users != turns || replies != turns {
		fail(t, "T5", fmt.Sprintf("stored history: users=%d replies=%d", users, replies))
		return
	}
	for _, event := range events {
		if event.Type == "context.compacted" && event.RunID != nil {
			pass(t, "T5", "200 turns stayed within the request limit and emitted compaction")
			return
		}
	}
	fail(t, "T5", "missing compaction event")
}
