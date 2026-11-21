package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

func TestResumeToolRounds(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		terminate bool
		failed    bool
	}{
		{name: "followup"},
		{name: "terminate", terminate: true},
		{name: "provider failure", failed: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var requests atomic.Int32
			followup := make(chan struct{})
			release := make(chan struct{})
			var released sync.Once
			unblock := func() { released.Do(func() { close(release) }) }
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch requests.Add(1) {
				case 1:
					sse(w, "OLD_ANSWER", nil)
				case 2:
					sse(w, "", []map[string]any{{"index": 0, "id": "resume-call", "type": "function", "function": map[string]string{"name": "state_probe", "arguments": "{}"}}})
				default:
					close(followup)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					if scenario.failed {
						http.Error(w, "fixture provider failure", http.StatusBadRequest)
						return
					}
					sse(w, "RESUMED_ANSWER", nil)
				}
			}))
			defer func() { unblock(); provider.Close() }()
			runtime := openDurable(t, provider)
			tool := &resultTool{result: agent.AgentToolResult{
				Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "tool result"}}, Terminate: scenario.terminate,
			}}
			session, err := runtime.Open(t.Context(), Definition{
				Agent: Agent{ID: "resume"}, ExtraTools: []agent.AgentTool{tool},
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.Send(t.Context(), turn("first"), nil); err != nil {
				t.Fatal(err)
			}
			queueTurn(t, session.(*durableSession), "resume")
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			type resumed struct {
				result Result
				err    error
			}
			done := make(chan resumed, 1)
			go func() {
				result, err := session.(*durableSession).Resume(ctx, 0, nil)
				done <- resumed{result, err}
			}()
			if !scenario.terminate {
				select {
				case result := <-done:
					t.Fatalf("resume returned before its tool follow-up: %+v, %v", result.result, result.err)
				case <-followup:
				case <-ctx.Done():
					t.Fatal("tool follow-up did not start")
				}
				unblock()
			}
			select {
			case resumed := <-done:
				if scenario.failed {
					if resumed.err == nil {
						t.Fatalf("resume ignored the provider failure: %+v", resumed.result)
					}
				} else if resumed.err != nil {
					t.Fatal(resumed.err)
				}
				want := "RESUMED_ANSWER"
				if scenario.terminate || scenario.failed {
					want = ""
				}
				if resumed.result.Text != want {
					t.Fatalf("resume text %q, want %q", resumed.result.Text, want)
				}
			case <-ctx.Done():
				t.Fatal("resume did not finish")
			}
			wantRequests := int32(3)
			if scenario.terminate {
				wantRequests = 2
			}
			if requests.Load() != wantRequests {
				t.Fatalf("provider requests %d, want %d", requests.Load(), wantRequests)
			}
		})
	}
}
