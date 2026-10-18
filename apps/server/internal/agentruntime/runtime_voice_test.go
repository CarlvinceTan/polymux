package agentruntime

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/testenv"
	"github.com/MichaelKinsy/PiG/durable/storage"
)

func TestEphemeralSessionNeverResumesOrPersists(t *testing.T) {
	testenv.PiG(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"private\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Private result\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"private\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	dir := t.TempDir()
	runtime, err := NewDurable(t.Context(), dir, provider.URL+"/v1", "local/test", "test-key", Client{}, storage.NewMemoryStorage())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	session, err := runtime.Open(t.Context(), Definition{Agent: contract.Agent{ID: "a", Name: "A"}, Tools: []string{"read"}, Scope: "voice-test", Ephemeral: true}, filepath.Join(dir, "missing.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := "Private task"
	result, err := session.Send(t.Context(), contract.Message{Parts: []contract.Part{{Kind: "text", Text: &text}}}, func(Delta) error { return nil })
	if err != nil || result.Text != "Private result" {
		t.Fatalf("private runtime %+v %v", result, err)
	}
	if session.Path() != "" {
		t.Fatal("private session has a persistence path", session.Path())
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".jsonl") || entry.IsDir() && entry.Name() == "sessions" {
			t.Errorf("private session created persistence: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
