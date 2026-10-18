package server

import (
	"context"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

const diffLimit = 200 << 10

func repoOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1")
	return cmd.Output()
}

func diffHTML(ctx context.Context, dir string) (string, error) {
	base, err := repoOutput(ctx, dir, "git", "merge-base", "origin/dev", "HEAD")
	if err != nil {
		base, err = repoOutput(ctx, dir, "git", "merge-base", "origin/main", "HEAD")
		if err != nil {
			return "", err
		}
	}
	stat, err := repoOutput(ctx, dir, "git", "diff", "--numstat", "--no-renames", "-z", strings.TrimSpace(string(base)))
	if err != nil {
		return "", err
	}
	files := []string{}
	added, removed := 0, 0
	for _, row := range strings.Split(string(stat), "\x00") {
		fields := strings.SplitN(row, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		a, _ := strconv.Atoi(fields[0])
		r, _ := strconv.Atoi(fields[1])
		added += a
		removed += r
		files = append(files, fields[2])
	}
	if len(files) == 0 {
		return "", nil
	}
	var body strings.Builder
	fmt.Fprintf(&body, `<h3>%d files, +%d −%d</h3><style>pre{white-space:pre-wrap;overflow-wrap:anywhere}summary{cursor:pointer}</style>`, len(files), added, removed)
	for i, file := range files {
		diff, err := repoOutput(ctx, dir, "git", "diff", "--no-ext-diff", "--no-renames", strings.TrimSpace(string(base)), "--", file)
		if err != nil {
			return "", err
		}
		fragment := "<details><summary>" + html.EscapeString(file) + "</summary><pre>" + html.EscapeString(string(diff)) + "</pre></details>"
		if body.Len()+len(fragment) > diffLimit-100 {
			fmt.Fprintf(&body, "<p>diff truncated, %d files more</p>", len(files)-i)
			break
		}
		body.WriteString(fragment)
	}
	return body.String(), nil
}

func (s *Server) diffCard(d agentruntime.Definition, run contract.Run) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	defer cancel()
	body, err := diffHTML(ctx, s.worktrees.Path(d.Agent.ID, agentruntime.WorkspaceThread(d)))
	if err != nil || body == "" {
		return
	}
	m := contract.Message{ID: ID(), ThreadID: run.ConversationID, Role: "assistant", AgentID: &run.AgentID, RunID: &run.ID, Status: "done", CreatedAt: store.Now()}
	s.refreshCard(ctx, &m, body)
}

func (s *Server) refreshCard(ctx context.Context, m *contract.Message, body string) {
	source := body + "<p>Committed changes stay in this detached checkout until integrated into dev.</p>"
	id := ID()
	path := filepath.Join(s.config.DataDir, "attachments", id)
	if os.WriteFile(path, []byte(source), 0600) != nil {
		return
	}
	text := htmlText(source)
	a := contract.Attachment{ID: id, Kind: "html", Name: "Changes.html", MimeType: "text/html", Size: int64(len(source)), URL: "/attachments/" + id + "/content", IndexingState: "ready", ExtractedText: &text}
	m.Parts = []contract.Part{{Kind: "attachment", Attachment: &a}}
	event := contract.Event{ConversationID: m.ThreadID, Type: "message.done", MessageID: &m.ID, AgentID: m.AgentID, RunID: m.RunID, Message: m}
	if _, err := s.Store.PublishAttachment(ctx, a, path, event, m); err != nil {
		os.Remove(path)
	}
}
