package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/graph-visualiser/internal/contract"
)

const (
	scanEvery = 30 * time.Second
	gitLimit  = 5 * time.Second
	ghLimit   = 15 * time.Second
	listLimit = 5
	// serverDevice is the app's node for the machine this service runs on.
	serverDevice = "device:server"
)

var githubRemote = regexp.MustCompile(`github\.com[:/]([\w.-]+/[\w.-]+?)(\.git)?$`)

// scanner keeps the latest look at the configured repositories.
type scanner struct {
	settings string
	log      *slog.Logger
	mu       sync.Mutex
	found    contract.WorkGraph
	// run is a seam for tests.
	run func(ctx context.Context, name string, args ...string) (string, error)
}

func newScanner(settings string, log *slog.Logger) *scanner {
	return &scanner{settings: settings, log: log, found: contract.WorkGraph{Nodes: []contract.WorkNode{}, Wires: []contract.WorkWire{}}, run: command}
}

func command(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).Output()
	return strings.TrimSpace(string(output)), err
}

func (s *scanner) start(ctx context.Context) {
	go func() {
		for {
			s.refresh(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(scanEvery):
			}
		}
	}()
}

func (s *scanner) latest() contract.WorkGraph {
	s.mu.Lock()
	defer s.mu.Unlock()
	return contract.WorkGraph{Revision: s.found.Revision, Nodes: append([]contract.WorkNode{}, s.found.Nodes...), Wires: append([]contract.WorkWire{}, s.found.Wires...)}
}

// options reads the plugin's settings, which restart this service when they change.
func (s *scanner) options() (paths []string, github bool) {
	github = true
	data, err := os.ReadFile(s.settings)
	if err != nil {
		return nil, github
	}
	var saved map[string]any
	if json.Unmarshal(data, &saved) != nil {
		return nil, github
	}
	if value, ok := saved["github"].(bool); ok {
		github = value
	}
	text, _ := saved["repositories"].(string)
	for _, path := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\n' }) {
		if path = expand(strings.TrimSpace(path)); path != "" {
			paths = append(paths, path)
		}
	}
	return paths, github
}

func expand(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return path
}

func (s *scanner) refresh(ctx context.Context) {
	paths, github := s.options()
	next := contract.WorkGraph{Nodes: []contract.WorkNode{}, Wires: []contract.WorkWire{}}
	seen := map[string]bool{}
	for _, path := range paths {
		repo, slug, ok := s.repository(ctx, path)
		if !ok {
			continue
		}
		next.Nodes = append(next.Nodes, repo)
		if slug == "" || !github {
			continue
		}
		node := "github:" + slug
		next.Wires = append(next.Wires, contract.WorkWire{ID: repo.ID + ">" + node, From: repo.ID, To: node,
			Direction: contract.WorkWireDirectionBoth, Line: contract.WorkWireLineDashed})
		if !seen[slug] {
			seen[slug] = true
			next.Nodes = append(next.Nodes, s.hosted(ctx, slug)...)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !same(s.found, next) {
		next.Revision = s.found.Revision + 1
		s.found = next
	}
}

func same(a, b contract.WorkGraph) bool {
	left, _ := json.Marshal([]any{a.Nodes, a.Wires})
	right, _ := json.Marshal([]any{b.Nodes, b.Wires})
	return string(left) == string(right)
}

// repository reads a checkout. Anything that isn't one stays off the graph.
func (s *scanner) repository(ctx context.Context, path string) (contract.WorkNode, string, bool) {
	git := func(args ...string) string {
		timed, cancel := context.WithTimeout(ctx, gitLimit)
		defer cancel()
		// A repository's own config mustn't get to run a command just because it was looked at.
		output, err := s.run(timed, "git", append([]string{"-C", path, "-c", "core.fsmonitor=false"}, args...)...)
		if err != nil {
			return ""
		}
		return output
	}
	commit := git("rev-parse", "--short", "HEAD")
	if commit == "" {
		return contract.WorkNode{}, "", false
	}
	branch := git("rev-parse", "--abbrev-ref", "HEAD")
	changed := 0
	if status := git("status", "--porcelain"); status != "" {
		changed = strings.Count(status, "\n") + 1
	}
	digest := sha256.Sum256([]byte(path))
	node := contract.WorkNode{ID: "repo:" + hex.EncodeToString(digest[:6]), Title: filepath.Base(path), Subtitle: branch + " at " + commit,
		Kind: "repo", Symbol: "folder", Parent: serverDevice,
		Details: []contract.WorkDetail{{Label: "Path", Value: path}, {Label: "Branch", Value: branch}, {Label: "Commit", Value: commit}}}
	if subject := git("log", "-1", "--format=%s"); subject != "" {
		node.Details = append(node.Details, contract.WorkDetail{Label: "Latest", Value: subject})
	}
	if changed > 0 {
		node.Blocks = append(node.Blocks, contract.WorkBlock{Kind: contract.WorkBlockKindBadge, Text: plural(changed, "change"), Tint: contract.WorkBlockTintOrange})
		node.Details = append(node.Details, contract.WorkDetail{Label: "Uncommitted", Value: plural(changed, "file")})
	}
	if counts := strings.Fields(git("rev-list", "--left-right", "--count", "@{upstream}...HEAD")); len(counts) == 2 {
		if behind, _ := strconv.Atoi(counts[0]); behind > 0 {
			node.Blocks = append(node.Blocks, contract.WorkBlock{Kind: contract.WorkBlockKindBadge, Text: fmt.Sprintf("%d behind", behind)})
		}
		if ahead, _ := strconv.Atoi(counts[1]); ahead > 0 {
			node.Blocks = append(node.Blocks, contract.WorkBlock{Kind: contract.WorkBlockKindBadge, Text: fmt.Sprintf("%d ahead", ahead)})
		}
	}
	slug := ""
	if match := githubRemote.FindStringSubmatch(git("remote", "get-url", "origin")); match != nil {
		slug = match[1]
	}
	return node, slug, true
}

func plural(count int, word string) string {
	if count == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", count, word)
}

// hosted reads a repository's GitHub state through gh, read-only. Without gh, the repository still shows.
func (s *scanner) hosted(ctx context.Context, slug string) []contract.WorkNode {
	root := "github:" + slug
	nodes := []contract.WorkNode{{ID: root, Title: slug, Kind: "github", Symbol: "arrow.triangle.branch"}}
	read := func(out any, args ...string) bool {
		timed, cancel := context.WithTimeout(ctx, ghLimit)
		defer cancel()
		output, err := s.run(timed, "gh", args...)
		if err != nil {
			s.log.Warn("read GitHub", "repository", slug, "error", err)
			return false
		}
		return json.Unmarshal([]byte(output), out) == nil
	}
	var pulls []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Branch  string `json:"headRefName"`
		IsDraft bool   `json:"isDraft"`
	}
	if read(&pulls, "pr", "list", "--repo", slug, "--limit", strconv.Itoa(listLimit), "--json", "number,title,headRefName,isDraft") {
		for _, pull := range pulls {
			state := contract.WorkNodeStateWorking
			if pull.IsDraft {
				state = contract.WorkNodeStateIdle
			}
			nodes = append(nodes, contract.WorkNode{ID: fmt.Sprintf("%s#%d", root, pull.Number), Parent: root, Title: pull.Title, Subtitle: fmt.Sprintf("Pull request %d", pull.Number),
				Kind: "pull", Symbol: "arrow.triangle.pull", State: state, Details: []contract.WorkDetail{{Label: "Branch", Value: pull.Branch}}})
		}
	}
	var runs []struct {
		ID         int64  `json:"databaseId"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Branch     string `json:"headBranch"`
	}
	if read(&runs, "run", "list", "--repo", slug, "--limit", strconv.Itoa(listLimit), "--json", "databaseId,name,status,conclusion,headBranch") {
		for _, run := range runs {
			nodes = append(nodes, contract.WorkNode{ID: fmt.Sprintf("%s/run/%d", root, run.ID), Parent: root, Title: run.Name, Subtitle: run.Branch,
				Kind: "check", Symbol: "checkmark.circle", State: checkState(run.Status, run.Conclusion)})
		}
	}
	return nodes
}

func checkState(status, conclusion string) string {
	switch {
	case status != "completed":
		return contract.WorkNodeStateWorking
	case conclusion == "success":
		return contract.WorkNodeStateDone
	case conclusion == "cancelled" || conclusion == "skipped":
		return contract.WorkNodeStateIdle
	}
	return contract.WorkNodeStateFailed
}
