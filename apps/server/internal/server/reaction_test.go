package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestReactionHTTP(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	m := contract.Message{ID: "target", ThreadID: "assistant", Role: "user", Status: "done", Parts: []contract.Part{}, CreatedAt: store.Now()}
	if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: m.ThreadID, Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, target, body string
		status, count      int
	}{
		{"add", "target", `{"emoji":"👍","remove":false}`, 200, 1},
		{"retry", "target", `{"emoji":"👍","remove":false}`, 200, 1},
		{"forged actor", "target", `{"emoji":"❤️","agentID":"assistant","remove":false}`, 400, 1},
		{"missing", "absent", `{"emoji":"👍","remove":false}`, 400, 1},
		{"invalid", "target", `{"emoji":"hello","remove":false}`, 400, 1},
		{"remove", "target", `{"emoji":"👍","remove":true}`, 200, 0},
		{"custom", "target", `{"emoji":"👩🏽‍💻","remove":false}`, 200, 1},
		{"custom retry", "target", `{"emoji":"👩🏽‍💻","remove":false}`, 200, 1},
		{"multiple emoji", "target", `{"emoji":"👍👍","remove":false}`, 400, 1},
		{"custom remove", "target", `{"emoji":"👩🏽‍💻","remove":true}`, 200, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRecorder()
			api.ServeHTTP(r, httptest.NewRequest(http.MethodPut, "/threads/assistant/messages/"+test.target+"/reaction", strings.NewReader(test.body)))
			if r.Code != test.status {
				t.Fatal(r.Code, r.Body.String())
			}
			got, err := api.Store.Message(t.Context(), m.ID)
			if err != nil || len(got.Reactions) != test.count || got.Seq != m.Seq {
				t.Fatal("unexpected stored reaction", got, err)
			}
			for _, reaction := range got.Reactions {
				if reaction.AgentID != nil {
					t.Fatal("client impersonated a bot", reaction)
				}
			}
		})
	}
	var jobs int
	if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("reacting started a model turn", jobs, err)
	}
}

func TestReactionTool(t *testing.T) {
	f := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		for range 2 {
			if _, err := botCall(ctx, d, "react_to_message", `{"emoji":"👍"}`); err != nil {
				return agentruntime.Result{}, err
			}
		}
		if _, err := botCall(ctx, d, "react_to_message", `{"message_id":"private","emoji":"❤️"}`); err == nil {
			t.Error("tool reacted outside its current chat")
		}
		return agentruntime.Result{}, nil
	}}
	api, _ := voiceTest(t, f)
	private := contract.Message{ID: "private", ThreadID: "b", Role: "user", Status: "done", Parts: []contract.Part{}, CreatedAt: store.Now()}
	if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: "b", Type: "message.done"}, &private); err != nil {
		t.Fatal(err)
	}
	botSend(t, api, "a", "react-turn", "Acknowledge this with a reaction only")
	run := awaitRun(t, api.Store, store.NewRun("react-turn", "a", "a", "message").ID, "completed")
	if run.Error != nil || run.Outcome == nil || *run.Outcome != "silent" {
		t.Fatal("reaction-only turn was treated as an empty reply", run)
	}
	messages, err := api.Store.Messages(t.Context(), "a", 0, 100)
	if err != nil || len(messages) != 1 || len(messages[0].Reactions) != 1 {
		t.Fatal("missing reaction or unwanted text reply", messages, err)
	}
	reaction := messages[0].Reactions[0]
	if reaction.Emoji != "👍" || reaction.AgentID == nil || *reaction.AgentID != "a" {
		t.Fatal("wrong reacting identity", reaction)
	}
}
