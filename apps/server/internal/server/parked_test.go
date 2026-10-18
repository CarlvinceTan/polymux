package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/testenv"
)

// A parked task must reappear on the next user turn, or the agent silently forgets it.
func TestParkedTaskSurfacesOnAUserTurn(t *testing.T) {
	testenv.PiG(t)
	var mu sync.Mutex
	var bodies []string
	var turns atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(json.RawMessage(mustRead(t, r)))
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if turns.Add(1) == 1 {
			calls := []map[string]any{{"index": 0, "id": "park", "type": "function", "function": map[string]string{
				"name": "manage_tasks", "arguments": `{"action":"checkpoint","goal":"Draft the fixture report","next":"Ask which format you want","wait_for_user":true}`,
			}}}
			data, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": calls}, "finish_reason": nil}}})
			fmt.Fprintf(w, "data: %s\n\n", data)
			fmt.Fprint(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Noted\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	runtime := durableRuntime(t, t.TempDir(), provider.URL+"/v1")
	api, endpoint := startTest(t, runtime)
	send := func(id string) {
		t.Helper()
		response, err := http.Post(endpoint.URL+"/threads/assistant/messages", "application/json",
			strings.NewReader(`{"id":"`+id+`","text":"Keep going","attachmentIDs":[]}`))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	send("first")
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		tasks, err := api.Store.Tasks(t.Context(), "assistant", "assistant")
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) > 0 && tasks[0].Status == "parked" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	tasks, err := api.Store.Tasks(t.Context(), "assistant", "assistant")
	if err != nil || len(tasks) == 0 || tasks[0].Status != "parked" {
		t.Fatalf("task did not park: %+v %v", tasks, err)
	}
	// The first turn answers once more after the tool result before the user's next turn.
	wait := func(n int32) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) && turns.Load() < n {
			time.Sleep(10 * time.Millisecond)
		}
		if turns.Load() < n {
			t.Fatalf("expected %d model requests, got %d", n, turns.Load())
		}
	}
	wait(2)
	send("second")
	wait(3)
	mu.Lock()
	defer mu.Unlock()
	if next := bodies[2]; !strings.Contains(next, "Tasks waiting on your answer") || !strings.Contains(next, "Draft the fixture report") {
		t.Fatalf("parked task was not surfaced: %s", next[:min(len(next), 600)])
	}
	for _, body := range bodies[:2] {
		if strings.Contains(body, "Tasks waiting on your answer") {
			t.Fatal("first turn claimed parked work")
		}
	}
}

func mustRead(t *testing.T, r *http.Request) []byte {
	t.Helper()
	defer r.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return nil
	}
	return raw
}
