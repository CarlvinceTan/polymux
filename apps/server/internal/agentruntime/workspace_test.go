package agentruntime

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// git runs a git command and returns its trimmed output, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

// gitRepo seeds a repository whose origin carries main and dev.
func gitRepo(t *testing.T, push bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "polymux@example.test")
	runGit(t, repo, "config", "user.name", "Polymux")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("polymux\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "Seed")
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", "main")
	runGit(t, repo, "remote", "add", "origin", origin)
	if push {
		runGit(t, repo, "push", "origin", "main")
		runGit(t, repo, "branch", "dev")
		runGit(t, repo, "push", "origin", "dev")
	}
	return repo
}

// seedFile commits a named file to both permanent branches.
func seedFile(t *testing.T, repo, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(name+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", name)
	runGit(t, repo, "commit", "-m", "Add "+name)
	runGit(t, repo, "push", "origin", "HEAD:main", "HEAD:dev")
}

// checkout prepares a conversation's worktree the way the server does before a turn.
func checkout(ctx context.Context, w *Worktrees, d Definition) (string, error) {
	return w.Ensure(ctx, d.Workspace, d.Agent.ID, WorkspaceThread(d))
}

func TestWorkspaceWorktreePerThread(t *testing.T) {
	repo := gitRepo(t, true)
	worktrees := NewWorktrees(t.TempDir())
	definition := func(scope string) Definition {
		return Definition{Agent: contract.Agent{ID: "coding", Name: "Coding", Model: "local/test"}, Prompt: "Work.", Scope: scope, Workspace: repo}
	}
	first, err := checkout(t.Context(), worktrees, definition("chat-one"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := checkout(t.Context(), worktrees, definition("chat-two"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("two threads share a checkout: %q", first)
	}
	for thread, path := range map[string]string{"chat-one": first, "chat-two": second} {
		if branch := runGit(t, path, "rev-parse", "--abbrev-ref", "HEAD"); branch != "HEAD" {
			t.Fatalf("thread %s is not detached: %q", thread, branch)
		}
		if head, want := runGit(t, path, "rev-parse", "HEAD"), runGit(t, repo, "rev-parse", "origin/dev"); head != want {
			t.Fatalf("thread %s does not start from origin/dev", thread)
		}
	}
	if branches := runGit(t, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads"); branches != "dev\nmain" {
		t.Fatalf("conversation checkouts created branches: %s", branches)
	}
	if err := os.WriteFile(filepath.Join(first, "draft.txt"), []byte("chat-one only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second, "draft.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a file written in one conversation is visible in another")
	}
	if again, err := checkout(t.Context(), worktrees, definition("chat-one")); err != nil || again != first {
		t.Fatalf("a later turn did not reuse the checkout: %q %v", again, err)
	}
}

func TestWorkspaceLiveDirectory(t *testing.T) {
	for _, name := range []string{"workspace", "absent"} {
		t.Run(name, func(t *testing.T) {
			runtime := openDurable(t, &httptest.Server{URL: "http://localhost"})
			d := Definition{Agent: contract.Agent{ID: "coding", Name: "Coding"}}
			want := filepath.Join(runtime.dir, "coding")
			if name == "workspace" {
				d.Worktree = t.TempDir()
				want = d.Worktree
			}
			session, err := runtime.Open(t.Context(), d, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			state, err := session.(*durableSession).conversation.Agent(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if state.Cwd == nil || *state.Cwd != want {
				t.Fatalf("live working directory = %v, want %q", state.Cwd, want)
			}
		})
	}
}

func TestWorkspaceFirstReply(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	d := Definition{Agent: contract.Agent{ID: "coding", Name: "Coding"}, Workspace: filepath.Join(home, "code", "Polymux"), Scope: "chat", Intro: true}
	prompt, err := buildPrompt(d, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Working in ~/code/Polymux in a detached conversation checkout") || !strings.Contains(prompt, "Do not create or push another branch") {
		t.Fatal("first reply does not identify the repository and detached workflow")
	}
}

func TestWorkspaceRemovedOnDelete(t *testing.T) {
	repo := gitRepo(t, true)
	worktrees := NewWorktrees(t.TempDir())
	path, err := worktrees.Ensure(t.Context(), repo, "coding", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "unfinished.txt"), []byte("work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := worktrees.Remove(t.Context(), repo, "coding", "chat"); err == nil {
		t.Fatal("a checkout with uncommitted work was removed")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the dirty checkout is gone: %v", err)
	}
	if err := os.Remove(filepath.Join(path, "unfinished.txt")); err != nil {
		t.Fatal(err)
	}
	if err := worktrees.Remove(t.Context(), repo, "coding", "chat"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the checkout survived deletion: %v", err)
	}
	if list := runGit(t, repo, "worktree", "list"); strings.Contains(list, path) {
		t.Fatalf("the repository still lists the deleted checkout: %s", list)
	}
	again, err := worktrees.Ensure(t.Context(), repo, "coding", "chat")
	if err != nil || again != path {
		t.Fatalf("the conversation could not start again: %q %v", again, err)
	}
	if err := os.WriteFile(filepath.Join(again, "finished.txt"), []byte("work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, again, "add", "finished.txt")
	runGit(t, again, "commit", "-m", "Finish work")
	head := runGit(t, again, "rev-parse", "HEAD")
	if err := worktrees.Remove(t.Context(), repo, "coding", "chat"); err == nil || !strings.Contains(err.Error(), "not integrated") {
		t.Fatalf("unintegrated commit was removed: %v", err)
	}
	runGit(t, repo, "update-ref", "refs/heads/dev", head)
	if err := worktrees.Remove(t.Context(), repo, "coding", "chat"); err != nil {
		t.Fatalf("integrated checkout was not removed: %v", err)
	}
}

func TestWorkspaceRemovedAfterDevRealignment(t *testing.T) {
	repo := gitRepo(t, true)
	worktrees := NewWorktrees(t.TempDir())
	path, err := worktrees.Ensure(t.Context(), repo, "coding", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "finished.txt"), []byte("work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "add", "finished.txt")
	runGit(t, path, "commit", "-m", "Finish work")
	head := runGit(t, path, "rev-parse", "HEAD")
	runGit(t, repo, "update-ref", "-m", "integrate", "refs/remotes/origin/dev", head)
	runGit(t, repo, "update-ref", "-m", "realign", "refs/remotes/origin/dev", "origin/main")
	if err := worktrees.Remove(t.Context(), repo, "coding", "chat"); err != nil {
		t.Fatalf("previously integrated checkout was not removed: %v", err)
	}
}

func TestWorkspaceDetachesLegacyCheckout(t *testing.T) {
	repo := gitRepo(t, true)
	worktrees := NewWorktrees(t.TempDir())
	path := worktrees.Path("coding", "chat")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "branch", "polymux/coding--chat", "origin/dev")
	runGit(t, repo, "worktree", "add", path, "polymux/coding--chat")
	if err := os.WriteFile(filepath.Join(path, "draft.txt"), []byte("unfinished\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := worktrees.Ensure(t.Context(), repo, "coding", "chat")
	if err != nil || got != path {
		t.Fatalf("legacy checkout was not reused: %q %v", got, err)
	}
	if branch := runGit(t, path, "branch", "--show-current"); branch != "" {
		t.Fatalf("legacy checkout remains on %q", branch)
	}
	if content, err := os.ReadFile(filepath.Join(path, "draft.txt")); err != nil || string(content) != "unfinished\n" {
		t.Fatalf("legacy work was not preserved: %q %v", content, err)
	}
}

func TestWorkspaceAddFailureExplained(t *testing.T) {
	repo := gitRepo(t, false) // origin has no main yet, so no worktree can be added
	worktrees := NewWorktrees(t.TempDir())
	_, err := worktrees.Ensure(t.Context(), repo, "coding", "chat")
	var repoErr *RepoError
	if !errors.As(err, &repoErr) {
		t.Fatalf("a failed worktree add was not explained: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "Can't open repo:") || !strings.Contains(err.Error(), "development branch") {
		t.Fatalf("the failure does not name the repository problem: %q", err)
	}
}

func TestWorkspaceTwoAgentsOneGroup(t *testing.T) {
	first, second := gitRepo(t, true), gitRepo(t, true)
	seedFile(t, first, "first.txt")
	seedFile(t, second, "second.txt")
	worktrees := NewWorktrees(t.TempDir())
	definition := func(agent, repo string) Definition {
		return Definition{Agent: contract.Agent{ID: agent, Name: agent, Model: "local/test"}, Prompt: "Work.", Scope: "group1", Workspace: repo}
	}
	firstPath, err := checkout(t.Context(), worktrees, definition("coding", first))
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := checkout(t.Context(), worktrees, definition("writer", second))
	if err != nil {
		t.Fatal(err)
	}
	if firstPath == secondPath {
		t.Fatalf("two agents in one group share a checkout: %q", firstPath)
	}
	for _, want := range []struct{ path, file string }{{firstPath, "first.txt"}, {secondPath, "second.txt"}} {
		if _, err := os.Stat(filepath.Join(want.path, want.file)); err != nil {
			t.Fatalf("checkout %q lacks its repo's file %s: %v", want.path, want.file, err)
		}
	}
	if _, err := os.Stat(filepath.Join(secondPath, "first.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an agent sees another repo's file in its checkout")
	}
}

func TestWorkspaceRejectsOtherRepoCheckout(t *testing.T) {
	first, second := gitRepo(t, true), gitRepo(t, true)
	worktrees := NewWorktrees(t.TempDir())
	path := worktrees.Path("coding", "chat")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	// The conversation's directory already holds another repo's checkout.
	runGit(t, second, "worktree", "add", "--detach", path, "origin/dev")
	_, err := worktrees.Ensure(t.Context(), first, "coding", "chat")
	var repoErr *RepoError
	if !errors.As(err, &repoErr) || !strings.HasPrefix(err.Error(), "Can't open repo:") {
		t.Fatalf("another repository's checkout was reused: %v", err)
	}
}

func TestWorkspaceRejectsLeftoverDirectory(t *testing.T) {
	repo := gitRepo(t, true)
	worktrees := NewWorktrees(t.TempDir())
	path := worktrees.Path("coding", "chat")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	// A failed worktree add leaves a directory that is not a checkout.
	if err := os.WriteFile(filepath.Join(path, "partial"), []byte("half\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := worktrees.Ensure(t.Context(), repo, "coding", "chat")
	var repoErr *RepoError
	if !errors.As(err, &repoErr) || !strings.HasPrefix(err.Error(), "Can't open repo:") {
		t.Fatalf("a leftover directory was reused: %v", err)
	}
}

func TestWorkspaceSameRepoTwoAgents(t *testing.T) {
	repo := gitRepo(t, true)
	legacy := runGit(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "branch", "polymux/group1", legacy)
	worktrees := NewWorktrees(t.TempDir())
	paths := make(map[string]string)
	for _, agent := range []string{"coding", "writer"} {
		path, err := worktrees.Ensure(t.Context(), repo, agent, "group1")
		if err != nil {
			t.Fatalf("agent %s: %v", agent, err)
		}
		paths[agent] = path
	}
	for agent, path := range paths {
		if branch := runGit(t, path, "branch", "--show-current"); branch != "" {
			t.Fatalf("agent %s is on branch %q", agent, branch)
		}
	}
	if paths["coding"] == paths["writer"] {
		t.Fatal("two agents share a checkout")
	}
	if err := os.WriteFile(filepath.Join(paths["coding"], "draft.txt"), []byte("coding only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths["writer"], "draft.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a file written by one agent is visible to the other")
	}
	if err := worktrees.Remove(t.Context(), repo, "writer", "group1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths["writer"]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed agent's checkout remains: %v", err)
	}
	if again, err := worktrees.Ensure(t.Context(), repo, "coding", "group1"); err != nil || again != paths["coding"] {
		t.Fatalf("deletion affected the other agent's checkout: %q %v", again, err)
	}
	if head := runGit(t, repo, "rev-parse", "polymux/group1"); head != legacy {
		t.Fatal("the legacy branch changed")
	}
}

func TestWorkspaceInvalidNames(t *testing.T) {
	worktrees := NewWorktrees(t.TempDir())
	for _, name := range []string{"", "bad/name", "bad\\name", "bad.name"} {
		for _, pair := range [][2]string{{name, "chat"}, {"coding", name}} {
			if _, err := worktrees.Ensure(t.Context(), "", pair[0], pair[1]); err == nil {
				t.Fatalf("Ensure accepted invalid pair %q", pair)
			}
			if err := worktrees.Remove(t.Context(), "", pair[0], pair[1]); err == nil {
				t.Fatalf("Remove accepted invalid pair %q", pair)
			}
		}
	}
}
