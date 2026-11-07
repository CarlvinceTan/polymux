package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable/storage/sqlite/node"
)

// heldTool ignores cancellation until released, like a tool that is slow to stop.
type heldTool struct {
	stateTool
	started chan struct{}
	release chan struct{}
}

func (*heldTool) Name() string { return "held_probe" }

func (*heldTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "held_probe", Description: "probe", Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": map[string]any{},
	}}
}

func (t *heldTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	close(t.started)
	<-t.release
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "held"}}}, nil
}

func sqliteDurable(t *testing.T, baseURL string, client Client) (*DurableRuntime, string) {
	t.Helper()
	dir := t.TempDir()
	storage, closeStorage, err := NewDurableStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeStorage() })
	runtime, err := NewDurable(t.Context(), dir, baseURL, "test-model", "test-key", client, storage)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime, dir
}

func conversationCount(t *testing.T, dir string) int64 {
	t.Helper()
	connection, err := node.NewDatabaseSync(filepath.Join(dir, "durable.db"), node.DatabaseSyncOptions{TimeoutMs: 5000})
	if err != nil {
		t.Fatal(err)
	}
	db := node.NewNodeSqliteDatabase(connection)
	defer db.Close()
	row, err := db.Get(`SELECT count(*) AS total FROM conversations`)
	if err != nil {
		t.Fatal(err)
	}
	total, _ := row["total"].(int64)
	return total
}

// A tool that outlives its rows would fail its next commit, and every conversation with it.
func TestForgetOutlastsRunningTool(t *testing.T) {
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			sse(w, "", []map[string]any{{"index": 0, "id": "call", "type": "function", "function": map[string]string{"name": "held_probe", "arguments": "{}"}}})
			return
		}
		sse(w, "READY", nil)
	}))
	defer provider.Close()
	runtime, dir := sqliteDurable(t, provider.URL+"/v1", Client{})
	tool := &heldTool{started: make(chan struct{}), release: make(chan struct{})}
	session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "held"}, ExtraTools: []agent.AgentTool{tool}}, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sent := make(chan error, 1)
	go func() {
		_, err := session.Send(ctx, turn("hold"), nil)
		sent <- err
	}()
	<-tool.started
	cancel()
	if err := <-sent; !errors.Is(err, context.Canceled) {
		t.Fatalf("send %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	forgotten := make(chan error, 1)
	go func() { forgotten <- runtime.Forget(t.Context(), session.Path()) }()
	select {
	case err := <-forgotten:
		close(tool.release)
		t.Fatalf("forgot a conversation with a running tool: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(tool.release)
	if err := <-forgotten; err != nil {
		t.Fatal(err)
	}
	if total := conversationCount(t, dir); total != 0 {
		t.Fatalf("conversations left %d", total)
	}
	other, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "other"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if result, err := other.Send(t.Context(), turn("still working"), nil); err != nil || result.Text != "READY" {
		t.Fatalf("runtime after forget %+v %v", result, err)
	}
}

func TestFailedOpenForgetsOnlyItsNewConversation(t *testing.T) {
	var broken atomic.Bool
	client := Client{BuildPrompt: func(d Definition, tools []string, down bool, skills []Skill) (string, error) {
		if broken.Load() {
			return "", errors.New("no prompt")
		}
		return buildPrompt(d, tools, down, skills)
	}}
	runtime, dir := sqliteDurable(t, "http://localhost/v1", client)
	definition := Definition{Agent: Agent{ID: "assistant"}}
	session, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal(err)
	}
	path := session.Path()
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	broken.Store(true)
	for _, attempt := range []string{"", path} {
		if _, err := runtime.Open(t.Context(), definition, attempt); err == nil {
			t.Fatal("open succeeded without a prompt")
		}
		if total := conversationCount(t, dir); total != 1 {
			t.Fatalf("after failed open of %q conversations = %d, want the saved one", attempt, total)
		}
	}
}
