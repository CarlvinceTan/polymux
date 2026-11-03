package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/testenv"
)

// A turn whose provider is out of allowance waits instead of failing, and the user's next message, as after a
// top-up, sets it going again.
func TestTurnWaitsForProvider(t *testing.T) {
	testenv.PiG(t)
	var spent atomic.Bool
	spent.Store(true)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if spent.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":{"type":"insufficient_quota"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Back again\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	runtime := durableRuntime(t, t.TempDir(), "")
	api, endpoint := startTest(t, runtime)
	if err := api.settings.SetProviders([]contract.Provider{{ID: "own", Name: "Own", Type: "custom", Enabled: true, BaseURL: provider.URL + "/v1", Models: []contract.ProviderModel{{ID: "chosen", Enabled: true}}}}); err != nil {
		t.Fatal(err)
	}
	runtime.SetProviders([]agentruntime.ProviderSpec{{ID: "own", Type: "custom", BaseURL: provider.URL + "/v1", Models: []string{"chosen"}}})
	profileRequest(t, api, http.MethodPatch, "/agents/assistant", `{"provider":"own","model":"chosen"}`, 200)
	send := func(id string) {
		t.Helper()
		response, err := http.Post(endpoint.URL+"/threads/assistant/messages", "application/json", strings.NewReader(`{"id":"`+id+`","text":"Hello","attachmentIDs":[]}`))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	await := func(what string, ok func(contract.Run) bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			runs, err := api.Store.Runs(t.Context(), "assistant")
			if err != nil {
				t.Fatal(err)
			}
			for _, run := range runs {
				if ok(run) {
					return
				}
			}
		}
		runs, _ := api.Store.Runs(t.Context(), "assistant")
		t.Fatalf("no run %s: %+v", what, runs)
	}
	send("first")
	await("waiting", func(run contract.Run) bool {
		return run.Status == contract.RunStatusRunning && strings.HasPrefix(run.Summary, "Waiting for a model provider")
	})
	spent.Store(false)
	send("topped-up")
	await("completed", func(run contract.Run) bool { return run.Status == contract.RunStatusCompleted })
	runs, _ := api.Store.Runs(t.Context(), "assistant")
	for _, run := range runs {
		if run.Status == contract.RunStatusFailed {
			t.Fatalf("a run failed while waiting: %+v", run)
		}
	}
}
