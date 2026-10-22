package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/testenv"
	"github.com/MichaelKinsy/PiG/agent"
)

func TestDirectoryTools(t *testing.T) {
	for _, test := range []struct {
		name   string
		tool   string
		args   string
		want   string
		failed bool
	}{
		{name: "list", tool: "ls", args: `{}`, want: "src/"},
		{name: "find", tool: "find", args: `{"pattern":"*.go"}`, want: "src/widget.go"},
		{name: "list missing", tool: "ls", args: `{"path":"missing"}`, want: "missing", failed: true},
		{
			name: "find missing", tool: "find", args: `{"path":"missing","pattern":"*.go"}`,
			want: "missing", failed: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			testenv.PiG(t)
			if test.tool == "find" {
				if _, err := exec.LookPath("fd"); err != nil {
					if _, err := exec.LookPath("fdfind"); err != nil {
						t.Skip("find requires fd or fdfind on PATH")
					}
				}
			}
			cwd := t.TempDir()
			if err := os.Mkdir(filepath.Join(cwd, "src"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cwd, "src", "widget.go"), []byte("package widget\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Tools    []struct{ Function struct{ Name string } }
					Messages json.RawMessage
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if len(body.Tools) != 1 || body.Tools[0].Function.Name != test.tool {
					t.Errorf("configured %s was not the offered tool: %+v", test.tool, body.Tools)
				}
				if requests.Add(1) == 1 {
					sse(w, "", []map[string]any{{
						"index": 0, "id": "directory", "type": "function",
						"function": map[string]string{"name": test.tool, "arguments": test.args},
					}})
					return
				}
				if !strings.Contains(string(body.Messages), test.want) {
					t.Errorf("model did not receive tool result %q: %s", test.want, body.Messages)
				}
				sse(w, "Checked.", nil)
			}))
			defer provider.Close()
			runtime := openDurable(t, provider)
			session, err := runtime.Open(t.Context(), Definition{
				Agent: contract.Agent{ID: "assistant"}, Tools: []string{test.tool}, Worktree: cwd,
				BeforeTool: func(context.Context, string, string, json.RawMessage) agent.ToolCallHookResult {
					return agent.ToolCallHookResult{}
				},
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			completed := []Delta{}
			result, err := session.Send(t.Context(), turn("Check the directory"), func(delta Delta) error {
				if delta.Kind == "tool.done" {
					completed = append(completed, delta)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Text != "Checked." || requests.Load() != 2 {
				t.Fatalf("turn did not finish after the tool: %+v, requests=%d", result, requests.Load())
			}
			if len(completed) != 1 || completed[0].Failed != test.failed {
				t.Fatalf("wrong tool outcome: %+v", completed)
			}
			if !strings.Contains(completed[0].Output, test.want) {
				t.Fatalf("tool did not use the session directory: %+v", completed[0])
			}
		})
	}
}

func TestFindWithoutBinary(t *testing.T) {
	testenv.PiG(t)
	t.Setenv("PATH", t.TempDir())
	tools, err := directoryTools(t.TempDir(), []string{"find"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tools[0].Execute(t.Context(), "missing", json.RawMessage(`{"pattern":"*.go"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(toolExcerpt(result.Content), "fd is not available") {
		t.Fatalf("missing fd did not return a tool error: %+v", result)
	}
	entries, err := os.ReadDir(os.Getenv("PIG_HOME"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("find changed tool home without a configured downloader: %v, %v", entries, err)
	}
}
