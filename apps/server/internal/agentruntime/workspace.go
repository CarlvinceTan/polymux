package agentruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// RepoError explains a workspace repository that could not be prepared. Its
// message is shown in the conversation, so a broken checkout never reads as a
// generic reply failure.
type RepoError struct {
	Err error
}

func (e *RepoError) Error() string { return "Can't open repo: " + e.Err.Error() }
func (e *RepoError) Unwrap() error { return e.Err }

// Worktrees prepares the per-conversation checkouts of agent workspaces. Each
// conversation gets its own worktree under one directory, and an existing
// checkout is reused, so a session reopened later keeps its files.
type Worktrees struct {
	dir string
	mu  sync.Mutex
}

func NewWorktrees(dir string) *Worktrees { return &Worktrees{dir: dir} }

func (d Definition) workingDir(fallback string) string {
	if d.Worktree != "" {
		return d.Worktree
	}
	return fallback
}

// Path is the checkout a conversation uses under this manager's directory. The
// name carries the agent as well as the thread, so agents working in one group
// thread never share a checkout.
func (w *Worktrees) Path(agent, thread string) string {
	return filepath.Join(w.dir, agent+"--"+thread)
}

// WorkspaceThread names the conversation a definition's checkout belongs to:
// the group thread when it has one, otherwise the agent's direct chat.
func WorkspaceThread(d Definition) string {
	if d.Scope != "" {
		return d.Scope
	}
	return d.Agent.ID
}

// Ensure returns the conversation's detached checkout of repo. The first call
// fetches origin and starts from origin/dev when present, otherwise the default
// main branch. Later calls reuse only a worktree of the same repository.
func (w *Worktrees) Ensure(ctx context.Context, repo, agent, thread string) (string, error) {
	if err := checkPair(agent, thread); err != nil {
		return "", err
	}
	path := w.Path(agent, thread)
	w.mu.Lock()
	defer w.mu.Unlock()
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return reuse(ctx, repo, path)
		}
		return "", &RepoError{Err: fmt.Errorf("%s is not a directory", path)}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", &RepoError{Err: err}
	}
	if err := os.MkdirAll(w.dir, 0700); err != nil {
		return "", &RepoError{Err: err}
	}
	if err := git(ctx, repo, "fetch", "origin"); err != nil {
		return "", err
	}
	base, err := workspaceBase(ctx, repo)
	if err != nil {
		return "", err
	}
	if err := git(ctx, repo, "worktree", "add", "--detach", path, base); err != nil {
		return "", err
	}
	return path, nil
}

// Remove deletes the conversation's checkout. A checkout with uncommitted work
// is left in place and the returned error says why; nothing is ever forced.
func (w *Worktrees) Remove(ctx context.Context, repo, agent, thread string) error {
	if err := checkPair(agent, thread); err != nil {
		return err
	}
	path := w.Path(agent, thread)
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if !integrated(ctx, repo, path) {
		return &RepoError{Err: errors.New("checkout has commits not integrated into dev or main")}
	}
	return git(ctx, repo, "worktree", "remove", path)
}

func workspaceBase(ctx context.Context, repo string) (string, error) {
	for _, ref := range []string{"origin/dev", "origin/main"} {
		if refExists(ctx, repo, "refs/remotes/"+ref) {
			return ref, nil
		}
	}
	ref, err := gitOutput(ctx, repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err == nil && ref != "" {
		return ref, nil
	}
	return "", &RepoError{Err: errors.New("origin has no development branch")}
}

func integrated(ctx context.Context, repo, path string) bool {
	head, err := gitOutput(ctx, path, "rev-parse", "HEAD")
	if err != nil {
		return false
	}
	for _, ref := range []string{"dev", "origin/dev", "main", "origin/main"} {
		name := refName(ref)
		if refExists(ctx, repo, name) && ancestor(ctx, repo, head, ref) {
			return true
		}
		history, historyErr := gitOutput(ctx, repo, "reflog", "show", "--format=%H", name)
		if historyErr != nil {
			continue
		}
		for commit := range strings.Lines(history) {
			if ancestor(ctx, repo, head, strings.TrimSpace(commit)) {
				return true
			}
		}
	}
	return false
}

func ancestor(ctx context.Context, repo, commit, ref string) bool {
	command := exec.CommandContext(ctx, "git", "-C", repo, "merge-base", "--is-ancestor", commit, ref)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return command.Run() == nil
}

func refName(ref string) string {
	if strings.HasPrefix(ref, "origin/") {
		return "refs/remotes/" + ref
	}
	return "refs/heads/" + ref
}

// reuse returns an existing checkout only when it is a worktree of repo. A
// directory left by another repository, or by a failed add, is never reused.
func reuse(ctx context.Context, repo, path string) (string, error) {
	checkout, err := commonDir(ctx, path)
	if err != nil {
		return "", &RepoError{Err: fmt.Errorf("%s is not a worktree of %s", path, repo)}
	}
	expected, err := commonDir(ctx, repo)
	if err != nil {
		return "", err
	}
	if checkout != expected {
		return "", &RepoError{Err: fmt.Errorf("%s is a worktree of another repository", path)}
	}
	if attached(ctx, path) {
		if err := git(ctx, path, "checkout", "--detach"); err != nil {
			return "", err
		}
	}
	return path, nil
}

func attached(ctx context.Context, path string) bool {
	command := exec.CommandContext(ctx, "git", "-C", path, "symbolic-ref", "--quiet", "HEAD")
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return command.Run() == nil
}

// commonDir is the resolved git common directory of dir, which names the
// repository a checkout belongs to. It fails for a directory that is not a
// worktree.
func commonDir(ctx context.Context, dir string) (string, error) {
	out, err := gitOutput(ctx, dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	common := out
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	if resolved, err := filepath.EvalSymlinks(common); err == nil {
		common = resolved
	}
	return filepath.Clean(common), nil
}

// checkPair rejects an agent or thread name that could leave the worktrees
// directory.
func checkPair(agent, thread string) error {
	for _, name := range []string{agent, thread} {
		if name == "" || strings.ContainsAny(name, "/\\.") {
			return &RepoError{Err: fmt.Errorf("invalid conversation name %q", name)}
		}
	}
	return nil
}

// git runs one git command in repo. A failure carries git's own message so the
// turn can explain what is wrong with the workspace.
func git(ctx context.Context, repo string, args ...string) error {
	_, err := gitOutput(ctx, repo, args...)
	return err
}

// gitOutput runs one git command in repo and returns its trimmed output.
func gitOutput(ctx context.Context, repo string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", &RepoError{Err: fmt.Errorf("git %s: %w", strings.Join(args, " "), ctxErr)}
		}
		message := strings.TrimSpace(output.String())
		if message == "" {
			message = err.Error()
		}
		return "", &RepoError{Err: fmt.Errorf("git %s: %s", strings.Join(args, " "), message)}
	}
	return strings.TrimSpace(output.String()), nil
}

// refExists probes the repository without creating anything.
func refExists(ctx context.Context, repo, ref string) bool {
	command := exec.CommandContext(ctx, "git", "-C", repo, "show-ref", "--verify", "--quiet", ref)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return command.Run() == nil
}
