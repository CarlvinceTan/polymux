package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable"
	"github.com/MichaelKinsy/PiG/durable/harness"
	"github.com/MichaelKinsy/PiG/durable/storage"
)

// turnMarker proves a tool sees the caller's turn context, which Polymux's tools rely on.
type turnMarker struct{}

// sse writes one scripted assistant turn in the OpenAI streaming shape.
func sse(w http.ResponseWriter, content string, calls []map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	delta := map[string]any{"role": "assistant"}
	finish := any(nil)
	if len(calls) > 0 {
		delta["tool_calls"] = calls
		finish = "tool_calls"
	} else {
		delta["content"] = content
		finish = "stop"
	}
	data, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
	fmt.Fprintf(w, "data: %s\n\n", data)
	fmt.Fprintf(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", finish)
}

func TestDurableHarnessRunsATurn(t *testing.T) {
	var toolRan bool
	var callerContextReachedTool bool
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		t.Logf("provider hit %d %s", hits.Load(), r.URL.Path)
		if hits.Load() == 1 {
			sse(w, "", []map[string]any{{"index": 0, "id": "call", "type": "function", "function": map[string]string{"name": "probe", "arguments": "{}"}}})
			return
		}
		sse(w, "READY", nil)
	}))
	defer provider.Close()

	registry := harness.CreateRegistry()
	probe := &durable.Extension{Name: "probe", Tools: []*durable.ToolRegistration{{
		ToolSchema: ai.ToolSchema{Name: "probe", Description: "probe", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}},
		Replay:     durable.ReplaySafe,
		Execute: func(ctx context.Context, _ any, _ durable.ToolExecutionApi) (durable.ToolExecutionResult, error) {
			toolRan = true
			callerContextReachedTool = ctx.Value(turnMarker{}) != nil
			return durable.ToolExecutionResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "probed"}}}, nil
		},
	}}}
	if err := registry.Install(probe); err != nil {
		t.Fatal(err)
	}
	opened, err := harness.OpenHarness(t.Context(), storage.NewMemoryStorage(), harness.HarnessOptions{
		Models: durableModels(provider.URL+"/v1", "test-key", "test-model", Client{}), Registry: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close(context.WithoutCancel(t.Context()))
	conversation, err := opened.Root(t.Context(), &harness.RootOptions{Agent: &harness.AgentChange{
		Model:        harness.SetTo(durable.ModelRef{Provider: DurableProvider, ModelId: "test-model"}),
		Instructions: harness.SetTo("Answer briefly."),
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(t.Context(), turnMarker{}, "polymux-turn")
	submission, err := conversation.Submit(ctx, durable.SubmissionDraft{Type: durable.SubmissionTypeInput, Content: ai.UserText("probe then answer")})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	settled, err := submission.Wait(waitCtx)
	if err != nil {
		t.Fatalf("wait: %v (provider hits: %d)", err, hits.Load())
	}
	if settled.Status != durable.SubmissionDone || settled.Answer == nil {
		t.Fatalf("submission = %+v", settled)
	}
	answer, err := answerOf(ctx, conversation, *settled.Answer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "READY") {
		t.Fatalf("answer = %q", answer)
	}
	if !toolRan {
		t.Fatal("tool did not run")
	}
	// Durable runs tools on its own task line, so a caller's context values do not reach
	// them. Polymux's turn state must come from the durable tool API instead of ctx.Value.
	if callerContextReachedTool {
		t.Log("caller context reached the tool; the ctx-based turn state could be reused")
	} else {
		t.Log("caller context did not reach the tool; turn state must be read from the durable API")
	}
}

// answerOf reads the text of the assistant entry a settled submission answered with.
func answerOf(ctx context.Context, conversation harness.Conversation, id durable.EntryId) (string, error) {
	value, err := conversation.Commit(ctx, func(tx durable.Tx) (any, error) {
		entry, err := durable.TxEntry(tx, durable.AssistantEntry, id)
		if err != nil || entry == nil {
			return (*durable.EntryRecord)(nil), err
		}
		return &entry.EntryRecord, nil
	})
	if err != nil {
		return "", err
	}
	entry, _ := value.(*durable.EntryRecord)
	if entry == nil {
		return "", nil
	}
	text := ""
	for _, message := range entry.Model {
		assistant, ok := message.(ai.AssistantMessage)
		if !ok {
			continue
		}
		for _, block := range assistant.Content {
			if content, ok := block.(ai.TextContent); ok {
				text += content.Text
			}
		}
	}
	return text, nil
}
