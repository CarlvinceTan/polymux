package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestReplyHTTPValidationAndReplay(t *testing.T) {
	f := &testFactory{}
	api, server := startTest(t, f)
	text := "Friday at noon."
	target := contract.Message{ID: "target", ThreadID: "assistant", Role: "assistant", Status: "done", CreatedAt: store.Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}
	if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: "assistant", Type: "message.done"}, &target); err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(server.URL+"/threads/assistant/messages", "application/json", strings.NewReader(`{"id":"reply","text":"When?","attachmentIDs":[],"replyToID":"target"}`))
	if err != nil {
		t.Fatal(err)
	}
	var message contract.Message
	err = json.NewDecoder(response.Body).Decode(&message)
	response.Body.Close()
	if response.StatusCode != 202 || err != nil || message.Reply == nil || message.Reply.Text != text {
		t.Fatalf("canonical quote: status=%d %+v %v", response.StatusCode, message.Reply, err)
	}
	for _, value := range []struct {
		name, body string
		status     int
	}{
		{"missing target", `{"id":"bad","text":"When?","attachmentIDs":[],"replyToID":"absent"}`, 400},
		{"same retry", `{"id":"reply","text":"When?","attachmentIDs":[],"replyToID":"target"}`, 200},
		{"changed target", `{"id":"reply","text":"When?","attachmentIDs":[]}`, 409},
	} {
		t.Run(value.name, func(t *testing.T) {
			response, err := http.Post(server.URL+"/threads/assistant/messages", "application/json", strings.NewReader(value.body))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != value.status {
				t.Fatalf("status %d want %d", response.StatusCode, value.status)
			}
			if value.status == 202 || value.status == 200 {
				var message contract.Message
				if err := json.NewDecoder(response.Body).Decode(&message); err != nil || message.Reply == nil || message.Reply.Text != text {
					t.Fatalf("canonical quote: %+v %v", message.Reply, err)
				}
			}
		})
	}
	awaitReply(t, api, "assistant", "reply")
	if f.calls.Load() != 1 {
		t.Fatalf("reply ran %d times", f.calls.Load())
	}
}
