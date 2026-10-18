package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func diffRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		diffGit(t, dir, args...)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	diffGit(t, dir, "add", ".")
	diffGit(t, dir, "commit", "-m", "Initial")
	diffGit(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	diffGit(t, dir, "update-ref", "refs/remotes/origin/dev", "HEAD")
	return dir
}
func diffGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git: %s: %v", out, err)
	}
}
func TestDiffCardOnlyWhenChanged(t *testing.T) {
	dir := diffRepo(t)
	body, err := diffHTML(t.Context(), dir)
	if err != nil || body != "" {
		t.Fatalf("clean card: %q %v", body, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	diffGit(t, dir, "add", ".")
	diffGit(t, dir, "commit", "-m", "Change")
	body, err = diffHTML(t.Context(), dir)
	if err != nil || !strings.Contains(body, "1 files, +1 −1") {
		t.Fatalf("changed card: %q %v", body, err)
	}
}
func TestDiffCardEscapesHTML(t *testing.T) {
	dir := diffRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("<script>alert('x')</script>\n"), 0600); err != nil {
		t.Fatal(err)
	}
	body, err := diffHTML(t.Context(), dir)
	if err != nil || strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("unsafe card: %q %v", body, err)
	}
	if issues := cardIssues(body); len(issues) != 0 {
		t.Fatal(issues)
	}
}
func TestDiffCardTruncates(t *testing.T) {
	dir := diffRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte(strings.Repeat("<", diffLimit)), 0600); err != nil {
		t.Fatal(err)
	}
	body, err := diffHTML(t.Context(), dir)
	if err != nil || len(body) > diffLimit || !strings.Contains(body, "diff truncated, 1 files more") {
		t.Fatalf("truncation: %d %v", len(body), err)
	}
}
func TestDiffCardExplainsDetachedCheckout(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api, err := New(t.Context(), db, Config{DataDir: dir, Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	if err := os.MkdirAll(api.worktrees.Path("assistant", "assistant"), 0700); err != nil {
		t.Fatal(err)
	}
	m := contract.Message{ID: ID(), ThreadID: "assistant", Role: "assistant", Status: "done", CreatedAt: store.Now()}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	api.refreshCard(ctx, &m, "<h3>Changes</h3>")
	messages, err := db.Messages(t.Context(), "assistant", 0, 100)
	if err != nil || len(messages) != 1 || messages[0].ID != m.ID {
		t.Fatalf("messages: %+v %v", messages, err)
	}
	_, path, err := db.Attachment(t.Context(), messages[0].Parts[0].Attachment.ID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(content), "until integrated into dev") {
		t.Fatalf("status: %s %v", content, err)
	}
}
