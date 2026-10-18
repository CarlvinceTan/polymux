package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable"
)

// A conversation must survive a harness restart, which is what makes the storage durable.
func TestDurableConversationSurvivesRestart(t *testing.T) {
	provider, requests := recordingProvider(t)
	dir := t.TempDir()
	definition := Definition{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}}

	storage, closeStorage, err := NewDurableStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewDurable(t.Context(), dir, provider.URL+"/v1", "test-model", "test-key", Client{}, storage)
	if err != nil {
		t.Fatal(err)
	}
	session, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal(err)
	}
	path := session.Path()
	if _, err := session.Send(t.Context(), turn("remember ALPHA"), nil); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := closeStorage(); err != nil {
		t.Fatal(err)
	}

	// A fresh harness over the same directory resumes the same conversation.
	reopened, closeAgain, err := NewDurableStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAgain()
	restarted, err := NewDurable(t.Context(), dir, provider.URL+"/v1", "test-model", "test-key", Client{}, reopened)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	resumed, err := restarted.Open(t.Context(), definition, path)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Path() != path {
		t.Fatalf("resumed %q want %q", resumed.Path(), path)
	}
	if _, err := resumed.Send(t.Context(), turn("what should you remember"), nil); err != nil {
		t.Fatal(err)
	}
	seen := requests()
	if len(seen) != 2 {
		t.Fatalf("requests %d", len(seen))
	}
	if !strings.Contains(seen[1], "ALPHA") {
		t.Fatalf("restarted harness lost the conversation: %s", seen[1])
	}
}

// A second message queued during the turn must not be the submission resume waits on.
func TestResumeIgnoresQueuedSubmission(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var answering atomic.Bool
	var hits atomic.Int32
	var mu sync.Mutex
	var bodies []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		encoded, _ := json.Marshal(body["messages"])
		mu.Lock()
		bodies = append(bodies, string(encoded))
		mu.Unlock()
		switch hits.Add(1) {
		case 1:
			sse(w, "", []map[string]any{{"index": 0, "id": "call", "type": "function", "function": map[string]string{"name": "state_probe", "arguments": "{}"}}})
		case 2:
			close(started)
			select {
			case <-release:
			case <-time.After(5 * time.Second):
				http.Error(w, "hold", http.StatusGatewayTimeout)
				return
			}
			http.Error(w, "stopped", http.StatusBadGateway)
		default:
			// Retries of the cancelled turn stay failed. Resume is the later request.
			if !answering.Load() {
				http.Error(w, "stopped", http.StatusBadGateway)
				return
			}
			sse(w, "from-first", nil)
		}
	}))
	t.Cleanup(provider.Close)
	dir := t.TempDir()
	storage, closeStorage, err := NewDurableStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStorage()
	runtime, err := NewDurable(t.Context(), dir, provider.URL+"/v1", "test-model", "test-key", Client{}, storage)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	session, err := runtime.Open(t.Context(), Definition{
		Agent: contract.Agent{ID: "assistant", Name: "Assistant"}, ExtraTools: []agent.AgentTool{&stateTool{}},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	var own atomic.Int64
	sendCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, sendErr := session.Send(sendCtx, turn("first"), func(delta Delta) error {
			if delta.Kind != "submission" {
				return nil
			}
			parsed, parseErr := strconv.ParseInt(delta.ToolID, 10, 64)
			own.Store(parsed)
			return parseErr
		})
		done <- sendErr
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("tool follow-up did not open")
	}
	queued := make(chan durable.Submission, 1)
	queueErr := make(chan error, 1)
	go func() {
		second, submitErr := session.(*durableSession).conversation.Submit(t.Context(), durable.SubmissionDraft{
			Type: durable.SubmissionTypeInput, Content: ai.UserText("SECOND_QUEUED"),
		})
		if submitErr != nil {
			queueErr <- submitErr
			return
		}
		queued <- second
	}()
	var second durable.Submission
	select {
	case second = <-queued:
	case err := <-queueErr:
		close(release)
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("second message did not queue while the turn was held")
	}
	close(release)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("send completed before cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("send did not return after cancel")
	}
	if own.Load() == 0 {
		t.Fatal("run did not record its submission")
	}
	queuedStatus, err := second.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if queuedStatus.Status != durable.SubmissionQueued {
		t.Fatalf("second submission status %s before resume", queuedStatus.Status)
	}
	answering.Store(true)
	result, err := session.(*durableSession).Resume(t.Context(), own.Load(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "from-first" {
		t.Fatalf("resume text %q", result.Text)
	}
	mu.Lock()
	seen := append([]string(nil), bodies...)
	mu.Unlock()
	if len(seen) < 3 {
		t.Fatalf("resume requests %d", len(seen))
	}
	resume := seen[len(seen)-1]
	if strings.Contains(resume, "SECOND_QUEUED") {
		t.Fatal("resume included the queued message")
	}
	if !strings.Contains(resume, "probed") {
		t.Fatal("resume request missing the stored tool result")
	}
	record, err := second.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if record.Status == durable.SubmissionDone {
		t.Fatal("resume answered the queued message")
	}
}
