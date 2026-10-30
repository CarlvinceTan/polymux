package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// openStream connects to a chat's stream at its current cursor. The deadline is well inside the
// 15 s heartbeat, so a stream that waits for the heartbeat instead of a write fails.
func openStream(t *testing.T, api *Server, url, thread string) (*http.Response, int64) {
	t.Helper()
	current, err := api.Store.Thread(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/threads/%s/stream?after=%d", url, thread, current.LastSeq), nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response, current.LastSeq
}

func TestStreamDeliversLiveWrites(t *testing.T) {
	api, web := startTest(t, &testFactory{})
	response, cursor := openStream(t, api, web.URL, "assistant")
	if err := api.Store.Presence(t.Context(), "assistant", true); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal("stream missed a live write", err)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event contract.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event.Seq != cursor+1 || event.Type != contract.EventTypeAgentPresence {
			t.Fatalf("stream skipped or replayed: %+v", event)
		}
		return
	}
}

func TestStreamEndsWhenChatIsDeleted(t *testing.T) {
	api, web := startTest(t, &testFactory{})
	if err := api.Store.Register(t.Context(), []contract.Agent{{ID: "helper", Name: "Helper"}}); err != nil {
		t.Fatal(err)
	}
	title := "Team"
	group, err := api.Store.CreateThread(t.Context(), "team", contract.CreateThreadRequest{AgentIDs: []string{"assistant", "helper"}, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	response, _ := openStream(t, api, web.URL, group.ID)
	if _, err := api.Store.DeleteGroup(t.Context(), group.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal("stream stayed open after its chat was deleted", err)
	}
}
