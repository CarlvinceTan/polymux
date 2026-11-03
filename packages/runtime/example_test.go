package runtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/packages/runtime"
)

func ExampleNewDurable() {
	// This local endpoint makes the example independent of a paid model service.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Header.Get("X-Application") != "example" || !strings.Contains(string(body["messages"]), "Host policy") {
			http.Error(w, "host configuration missing", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"example\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hello.\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"example\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	dir, err := os.MkdirTemp("", "agent-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	storage, closeStorage, err := runtime.NewDurableStorage(dir)
	if err != nil {
		panic(err)
	}
	defer closeStorage()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := runtime.Client{
		Headers: map[string]string{"X-Application": "example"},
		BuildPrompt: func(d runtime.Definition, _ []string, _ bool, _ []runtime.Skill) (string, error) {
			return "Host policy: " + d.SystemPrompt(), nil
		},
	}
	runner, err := runtime.NewDurable(ctx, dir, provider.URL+"/v1", "example-model", "example-key", client, storage)
	if err != nil {
		panic(err)
	}
	defer runner.Close()
	definition := runtime.Definition{Agent: runtime.Agent{ID: "assistant"}, Prompt: "Answer briefly."}
	path := ""
	for _, text := range []string{"Hello", "Continue"} {
		session, err := runner.Open(ctx, definition, path)
		if err != nil {
			panic(err)
		}
		path = session.Path()
		result, err := session.Send(ctx, runtime.Message{Parts: []runtime.Part{{Text: &text}}}, nil)
		if err != nil {
			panic(err)
		}
		fmt.Println(result.Text)
		if err := session.Close(); err != nil {
			panic(err)
		}
	}
	// Output:
	// Hello.
	// Hello.
}
