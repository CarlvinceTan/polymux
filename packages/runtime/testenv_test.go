package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable"
)

func isolatePiG(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PIG_HOME", root)
	t.Setenv("PIG_CODING_AGENT_DIR", filepath.Join(root, "agent"))
	for _, name := range []string{"PIG_USE_PI_DIRS", "PI_CODING_AGENT_DIR", "PIG_CODING_AGENT_SESSION_DIR", "PI_CODING_AGENT_SESSION_DIR", "PIG_TEST_FAUX"} {
		t.Setenv(name, "")
	}
}

type testCredentials struct {
	Secrets modelSecrets
	mu      sync.Mutex
	values  map[string]*ai.Credential
}

func (c *testCredentials) read(id string) *ai.Credential {
	if value := c.values[id]; value != nil {
		out := *value
		return &out
	}
	if key := c.Secrets[strings.TrimPrefix(id, "settings:")]; key != "" {
		return &ai.Credential{Type: ai.CredentialAPIKey, Key: key}
	}
	return nil
}
func (c *testCredentials) Read(_ context.Context, id string) (*ai.Credential, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.read(id), nil
}
func (c *testCredentials) Modify(ctx context.Context, id string, fn func(*ai.Credential) (*ai.Credential, error)) (*ai.Credential, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := fn(c.read(id))
	if err != nil {
		return nil, err
	}
	if c.values == nil {
		c.values = map[string]*ai.Credential{}
	}
	if value != nil {
		saved := *value
		c.values[id] = &saved
	}
	return value, nil
}
func (c *testCredentials) Delete(_ context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.values, id)
	return nil
}
func (c *testCredentials) List(context.Context) ([]ai.CredentialInfo, error) {
	return []ai.CredentialInfo{}, nil
}

// turnMarker proves a tool sees the caller's turn context, which Polymux's tools rely on.
type turnMarker struct{}

// queueTurn leaves a user entry the next Resume answers, as a turn interrupted before it ran would.
func queueTurn(t *testing.T, session *durableSession, text string) {
	t.Helper()
	conversation := session.conversation.Id()
	_, err := session.conversation.Commit(t.Context(), func(tx durable.Tx) (any, error) {
		_, err := durable.TxAppendEntry(tx, durable.UserEntry, conversation, durable.TypedEntryDraft[durable.Never]{
			Model: []ai.Message{ai.UserMessage{Content: ai.UserText(text), Timestamp: time.Now().UnixMilli()}},
		})
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
}

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
