package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/graph-visualiser/internal/contract"
)

func started(t *testing.T) (*service, http.Handler) {
	t.Helper()
	reports, err := openBook(filepath.Join(t.TempDir(), "reports.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &service{book: reports, scan: newScanner(filepath.Join(t.TempDir(), "settings.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))}
	return s, s.handler("token")
}

func post(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest("POST", path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestRefusesWithoutToken(t *testing.T) {
	_, handler := started(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/graph", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal(response.Code)
	}
}

func TestReportToolStoresWorkForItsChat(t *testing.T) {
	s, handler := started(t)
	args := `{"nodes":[{"id":"claude-1","title":"Claude Code","parent":"me","kind":"agent"}],"wires":[{"from":"me","to":"claude-1","label":"Fix the bug"}]}`
	response := post(t, handler, "/tools/report_work", `{"chat":"chat-1","agent":"linus","args":`+args+`}`)
	var answer map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil || answer["text"] != "Reported 1 nodes and 1 wires." {
		t.Fatalf("%s %v", response.Body, err)
	}
	graph := s.merged()
	if len(graph.Nodes) != 1 || graph.Nodes[0].Parent != "agent:linus" || graph.Nodes[0].Chats[0] != "chat-1" || graph.Nodes[0].UpdatedAt == "" {
		t.Fatalf("%+v", graph.Nodes)
	}
	if len(graph.Wires) != 1 || graph.Wires[0].From != "agent:linus" || graph.Wires[0].ID != "agent:linus>claude-1" {
		t.Fatalf("%+v", graph.Wires)
	}
	post(t, handler, "/tools/report_work", `{"chat":"chat-1","agent":"linus","args":{"nodes":[{"id":"claude-1","title":"Claude Code","state":"done"}]}}`)
	if graph = s.merged(); len(graph.Nodes) != 1 || graph.Nodes[0].State != "done" || graph.Revision != 2 {
		t.Fatalf("%+v", graph)
	}
	post(t, handler, "/tools/report_work", `{"agent":"linus","args":{"remove":["claude-1","agent:linus>claude-1"]}}`)
	if graph = s.merged(); len(graph.Nodes) != 0 || len(graph.Wires) != 0 {
		t.Fatalf("%+v", graph)
	}
}

func TestBadReportsExplainThemselves(t *testing.T) {
	s, handler := started(t)
	for body, want := range map[string]string{
		`{"args":{"nodes":[{"id":"","title":"x"}]}}`:               "short ID",
		`{"args":{"nodes":[{"id":"a"}]}}`:                          "short title",
		`{"args":{"nodes":[{"id":"a","title":"x","parent":"a"}]}}`: "own parent",
		`{"args":{"nodes":[{"id":"a","title":"x","details":[{"label":"l","value":"` + strings.Repeat("v", 600) + `"}]}]}}`: "too much content",
		`{"args":{"wires":[{"from":"a"}]}}`: "from and to",
		`{"args":"nope"}`:                   "can't be read",
	} {
		if response := post(t, handler, "/tools/report_work", body); !strings.Contains(response.Body.String(), want) {
			t.Errorf("%s: %s", body, response.Body)
		}
	}
	if graph := s.merged(); len(graph.Nodes) != 0 {
		t.Fatalf("%+v", graph)
	}
}

// A repository's config can name a command for git status to run, and looking at it mustn't run it.
func TestScanIgnoresARepositorysMonitorCommand(t *testing.T) {
	dir := gitRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	hook := filepath.Join(t.TempDir(), "monitor.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", dir, "config", "core.fsmonitor", hook).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	var calls []string
	s := scripted(t, map[string]any{"repositories": dir, "github": false}, &calls)
	s.refresh(t.Context())
	if len(s.latest().Nodes) != 1 {
		t.Fatalf("%+v", s.latest())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the repository's monitor command ran")
	}
}

func TestReportsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports.json")
	first, err := openBook(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.apply(contract.WorkReport{Nodes: []contract.WorkNode{{ID: "a", Title: "A"}}}, "", ""); err != nil {
		t.Fatal(err)
	}
	second, err := openBook(path)
	if err != nil {
		t.Fatal(err)
	}
	if graph := second.graph(); len(graph.Nodes) != 1 || graph.Revision != 1 {
		t.Fatalf("%+v", graph)
	}
}

func TestOldReportsExpire(t *testing.T) {
	b, err := openBook(filepath.Join(t.TempDir(), "reports.json"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	b.data.Nodes = []contract.WorkNode{{ID: "old", Title: "Old", UpdatedAt: old}, {ID: "new", Title: "New", UpdatedAt: time.Now().UTC().Format(time.RFC3339)}}
	if graph := b.graph(); len(graph.Nodes) != 1 || graph.Nodes[0].ID != "new" {
		t.Fatalf("%+v", graph.Nodes)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "polymux")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.com"}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, output)
		}
	}
	run("init", "-b", "dev")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "First commit")
	run("remote", "add", "origin", "git@github.com:example/polymux.git")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Only gh is scripted, so no test reaches a real account.
func scripted(t *testing.T, settings map[string]any, calls *[]string) *scanner {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s := newScanner(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.run = func(ctx context.Context, name string, args ...string) (string, error) {
		if name != "gh" {
			return command(ctx, name, args...)
		}
		*calls = append(*calls, strings.Join(args, " "))
		if args[0] == "pr" {
			return `[{"number":7,"title":"Add the graph","headRefName":"graph","isDraft":false}]`, nil
		}
		return `[{"databaseId":9,"name":"Tests","status":"completed","conclusion":"failure","headBranch":"dev"}]`, nil
	}
	return s
}

func TestScanReadsRepositoriesAndGitHub(t *testing.T) {
	dir := gitRepo(t)
	var calls []string
	s := scripted(t, map[string]any{"repositories": dir + ", /no/such/repo"}, &calls)
	s.refresh(t.Context())
	graph := s.latest()
	if graph.Revision != 1 || len(graph.Nodes) != 4 || len(graph.Wires) != 1 {
		t.Fatalf("%+v", graph)
	}
	repo := graph.Nodes[0]
	if repo.Title != "polymux" || repo.Parent != serverDevice || !strings.HasPrefix(repo.Subtitle, "dev at ") || repo.Blocks[0].Text != "1 change" {
		t.Fatalf("%+v", repo)
	}
	if graph.Wires[0].To != "github:example/polymux" || graph.Wires[0].Direction != "both" {
		t.Fatalf("%+v", graph.Wires)
	}
	if graph.Nodes[2].Title != "Add the graph" || graph.Nodes[2].Parent != "github:example/polymux" || graph.Nodes[3].State != "failed" {
		t.Fatalf("%+v", graph.Nodes)
	}
	for _, call := range calls {
		if !strings.HasPrefix(call, "pr list") && !strings.HasPrefix(call, "run list") {
			t.Fatalf("a scan only reads GitHub: %s", call)
		}
	}
	s.refresh(t.Context())
	if s.latest().Revision != 1 {
		t.Fatal("an unchanged scan moved the revision")
	}
}

func TestScanSkipsGitHubWhenTurnedOff(t *testing.T) {
	var calls []string
	s := scripted(t, map[string]any{"repositories": gitRepo(t), "github": false}, &calls)
	s.refresh(t.Context())
	if graph := s.latest(); len(graph.Nodes) != 1 || len(graph.Wires) != 0 || len(calls) != 0 {
		t.Fatalf("%+v %v", graph, calls)
	}
}

func TestScanSurvivesMissingGitHub(t *testing.T) {
	dir := gitRepo(t)
	var calls []string
	s := scripted(t, map[string]any{"repositories": dir}, &calls)
	base := s.run
	s.run = func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "gh" {
			return "", exec.ErrNotFound
		}
		return base(ctx, name, args...)
	}
	s.refresh(t.Context())
	if graph := s.latest(); len(graph.Nodes) != 2 || graph.Nodes[1].ID != "github:example/polymux" {
		t.Fatalf("%+v", graph.Nodes)
	}
}

func TestReadToolListsIDs(t *testing.T) {
	s, handler := started(t)
	var answer map[string]string
	if err := json.Unmarshal(post(t, handler, "/tools/read_work", `{}`).Body.Bytes(), &answer); err != nil || answer["text"] != "Nothing has been reported." {
		t.Fatalf("%v %v", answer, err)
	}
	if err := s.book.apply(contract.WorkReport{Nodes: []contract.WorkNode{{ID: "repo:1", Title: "polymux", Kind: "repo"}}}, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(post(t, handler, "/tools/read_work", `{}`).Body.Bytes(), &answer); err != nil || answer["text"] != "repo:1: polymux (repo)" {
		t.Fatalf("%v %v", answer, err)
	}
}

func TestChecksReadAsStates(t *testing.T) {
	for _, item := range [][3]string{{"in_progress", "", "working"}, {"completed", "success", "done"}, {"completed", "cancelled", "idle"}, {"completed", "failure", "failed"}} {
		if got := checkState(item[0], item[1]); got != item[2] {
			t.Errorf("%v: %s", item, got)
		}
	}
}
