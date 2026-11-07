package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceTaskCleanup(t *testing.T) {
	repo := workspaceRepo(t)
	api, _ := startTest(t, &testFactory{})
	patchWorkspace(t, api, "assistant", repo)
	checkout := func(owner, scope string) string {
		t.Helper()
		path, err := api.worktrees.Ensure(t.Context(), repo, owner, scope)
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	child := checkout("assistant", "child-clean")
	peer := checkout("assistant", "peer-clean")
	dirty := checkout("assistant", "child-dirty")
	result := checkout("assistant", "child-result")
	other := checkout("assistant-other", "child-clean")
	if err := os.WriteFile(filepath.Join(dirty, "draft.txt"), []byte("Unfinished child work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(result, "result.txt"), []byte("Unintegrated child result\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, result, "add", "result.txt")
	git(t, result, "commit", "-m", "Complete child task")
	head := git(t, result, "rev-parse", "HEAD")
	if git(t, result, "status", "--porcelain") != "" {
		t.Fatal("child result fixture has uncommitted changes")
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/agents/assistant", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	for _, path := range []string{child, peer} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("clean task checkout remains at %s: %v", path, err)
		}
	}
	for path, want := range map[string]string{
		filepath.Join(dirty, "draft.txt"):   "Unfinished child work\n",
		filepath.Join(result, "result.txt"): "Unintegrated child result\n",
		filepath.Join(other, "README.md"):   "polymux\n",
	} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("preserved checkout lost work at %s: %q %v", path, data, err)
		}
	}
	if got := git(t, result, "rev-parse", "HEAD"); got != head {
		t.Fatalf("unintegrated child commit changed: got %s, want %s", got, head)
	}
	if got := git(t, other, "rev-parse", "HEAD"); got != git(t, repo, "rev-parse", "origin/main") {
		t.Fatal("another owner's clean task checkout changed")
	}
}
