package bench

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/server"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

// Message is something the user saw.
type Message struct {
	At       float64 `json:"at"`
	Channel  string  `json:"channel"`
	From     string  `json:"from,omitempty"` // the bot that wrote it, where a system has several
	Text     string  `json:"text"`
	Question bool    `json:"question,omitempty"`
}

// Env is what a run gives the system under test.
type Env struct {
	Dir      string
	Model    string // the pinned model name to request
	ModelURL string // this run's proxy base
	World    *World
	Start    time.Time
}

// System is a harness under test. Channel "main" is the main chat; other channels
// are the system's own threads, named as Seen reports them.
type System interface {
	Name() string
	Start(ctx context.Context, env Env) error
	Send(channel, text string) error
	Event(text string) error
	Seen() ([]Message, error)
	Close()
}

const botID = "assistant"

// team is the group suite's specialists: Ava leads, and each of the others owns one part of Ana's life.
var team = []struct{ id, name, role, tools, brief string }{
	{botID, "Ava", "Personal assistant", "lead", "You're Ava, Ana Ruiz's personal assistant: her email and calendar. Finn looks after her money, Theo her trips, activities and restaurants, and Iris her studies. When something is theirs, leave it to them or ask them rather than guessing."},
	{"finn", "Finn", "Finance", "finance", "You're Finn, Ana Ruiz's finance assistant: her bank account, spending and bills. You can't book anything or see her calendar."},
	{"theo", "Theo", "Travel and dining", "travel", "You're Theo, Ana Ruiz's travel and dining assistant: trips, activities and restaurant bookings. You can't see her calendar or her bank account."},
	{"iris", "Iris", "Study coach", "study", "You're Iris, Ana Ruiz's study coach for her Cobalt University courses: the student portal, deadlines and study time in her calendar."},
}

// Polymux runs the real Polymux server in-process against the run's proxy and world. With Team it runs the
// specialists of the group suite in one group chat instead of a single bot.
type Polymux struct {
	Team    bool
	group   string            // the team's group chat
	filled  map[string]bool   // secret cards the user has filled
	names   map[string]string // bot names by ID
	env     Env
	first   map[string]float64 // when each finished message was first seen
	db      *store.Store
	release func() error
	runtime *agentruntime.DurableRuntime
	api     *server.Server
	http    *httptest.Server
}

func (p *Polymux) Name() string {
	if p.Team {
		return "polymux-team"
	}
	return "polymux"
}

func (p *Polymux) Start(ctx context.Context, env Env) error {
	p.env, p.first, p.filled = env, map[string]float64{}, map[string]bool{}
	dir := filepath.Join(env.Dir, "polymux")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	db, err := store.Open(ctx, filepath.Join(dir, "db"))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	p.db = db
	storage, release, err := agentruntime.NewDurableStorage(dir)
	if err != nil {
		return fmt.Errorf("open durable storage: %w", err)
	}
	p.release = release
	p.runtime, err = agentruntime.NewDurable(ctx, dir, env.ModelURL, env.Model, "bench", agentruntime.Client{Window: agentruntime.DefaultWindow}, storage)
	if err != nil {
		return fmt.Errorf("start runtime: %w", err)
	}
	bots := []agentruntime.Definition{{
		Agent: contract.Agent{ID: botID, Name: "Polly", Model: env.Model},
		Tools: []string{},
		MCP:   []agentruntime.MCPServer{{Name: "world", URL: env.World.URL}},
	}}
	p.names = map[string]string{botID: "Polly"}
	if p.Team {
		bots = nil
		for _, b := range team {
			bots = append(bots, agentruntime.Definition{Agent: contract.Agent{ID: b.id, Name: b.name, Role: b.role, Model: env.Model}, Tools: []string{},
				Prompt: agentruntime.DefaultPrompt + "\n\n" + b.brief, MCP: []agentruntime.MCPServer{{Name: "world", URL: env.World.RoleURL(b.tools)}}})
			p.names[b.id] = b.name
		}
	}
	p.api, err = server.New(ctx, db, server.Config{DataDir: dir, Model: env.Model, Definitions: bots,
		Factory: p.runtime, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	p.http = httptest.NewServer(p.api)
	// Ana is in New York; the app reports its device's timezone, so the bot knows her local date and time.
	if err := p.call(http.MethodPut, "/client", contract.ClientState{Platform: contract.ClientStatePlatformMacos, Timezone: "America/New_York", Capabilities: []string{}}, nil); err != nil {
		return fmt.Errorf("report client: %w", err)
	}
	if p.Team {
		ids := make([]string, 0, len(team))
		for _, b := range team {
			ids = append(ids, b.id)
		}
		title := "Ana's helpers"
		var thread contract.Thread
		if err := p.call(http.MethodPost, "/threads", contract.CreateThreadRequest{AgentIDs: ids, Title: &title}, &thread); err != nil {
			return fmt.Errorf("create group chat: %w", err)
		}
		p.group = thread.ID
	}
	// Outside events reach the bot the way a watcher's would.
	return db.SaveTrigger(ctx, store.Trigger{ID: "inbox", AgentID: botID, Source: "email", Enabled: true,
		Prompt: "Watch the user's email and notifications, and tell them only what they would want to know."})
}

func (p *Polymux) Send(channel, text string) error {
	thread := botID
	if id, ok := strings.CutPrefix(channel, "task:"); ok {
		thread = id
	} else if channel == "group" && p.group != "" {
		thread = p.group
	}
	return p.post("/threads/"+thread+"/messages", contract.SendRequest{ID: newID(), Text: text, AttachmentIDs: []string{}})
}

func (p *Polymux) Event(text string) error {
	return p.post("/triggers/inbox/events", store.TriggerEvent{ID: newID(), Source: "email", Text: text})
}

func (p *Polymux) post(path string, body any) error { return p.call(http.MethodPost, path, body, nil) }

func (p *Polymux) call(method, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(method, p.http.URL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		detail, _ := io.ReadAll(response.Body)
		return fmt.Errorf("%s: HTTP %d: %s", path, response.StatusCode, strings.TrimSpace(string(detail)))
	}
	if out != nil {
		return json.NewDecoder(response.Body).Decode(out)
	}
	return nil
}

// Routines lists every bot's scheduled work.
func (p *Polymux) Routines() []contract.Routine {
	var out []contract.Routine
	for id := range p.names {
		if routines, err := p.db.Routines(context.Background(), id); err == nil {
			out = append(out, routines...)
		}
	}
	return out
}

func (p *Polymux) Seen() ([]Message, error) {
	ctx := context.Background()
	threads, err := p.db.Threads(ctx)
	if err != nil {
		return nil, err
	}
	var out []Message
	for _, thread := range threads {
		channel := "main"
		switch {
		case thread.Task != nil:
			channel = "task:" + thread.ID
		case thread.ID == p.group && p.group != "":
			channel = "group"
		case thread.ID != botID:
			continue
		}
		messages, err := p.db.Messages(ctx, thread.ID, 0, 1000)
		if err != nil {
			return nil, err
		}
		for _, m := range messages {
			if m.Role != contract.MessageRoleAssistant {
				continue
			}
			p.fill(thread.ID, m)
			if m.Status != contract.MessageStatusDone {
				continue
			}
			text, question := p.partsText(m.Parts)
			if strings.TrimSpace(text) == "" {
				continue
			}
			// A turn's own message is created when the turn starts, so time it from when it was first seen finished.
			at, ok := p.first[m.ID]
			if !ok {
				at = time.Since(p.env.Start).Seconds()
				p.first[m.ID] = at
			}
			from := ""
			if m.AgentID != nil && p.Team {
				from = p.names[*m.AgentID]
			}
			out = append(out, Message{At: at, Channel: channel, From: from, Text: text, Question: question})
		}
	}
	return out, nil
}

// fill types a password into a pending masked secret card, as the user would.
func (p *Polymux) fill(thread string, m contract.Message) {
	for _, part := range m.Parts {
		if part.Credential != nil && part.Credential.Status == "pending" && !p.filled[m.ID] {
			p.filled[m.ID] = true
			value := "Ana-Ruiz-2026!"
			_ = p.post("/threads/"+thread+"/messages/"+m.ID+"/credential", contract.CredentialAnswer{Value: &value})
		}
	}
}

// Busy reports queued or running turns, which a stalled turn keeps up even when nothing else is happening.
func (p *Polymux) Busy() bool {
	var n int
	err := p.db.DB.QueryRow(`SELECT count(*) FROM jobs WHERE state IN ('queued','running')`).Scan(&n)
	return err == nil && n > 0
}

// tags strips markup but keeps script text, where interactive cards hold what their tabs show.
var tags = regexp.MustCompile(`(?s)<style.*?</style>|<[^>]+>`)

func (p *Polymux) partsText(parts []contract.Part) (string, bool) {
	var texts []string
	question := false
	for _, part := range parts {
		switch {
		case part.Kind == contract.PartKindText && part.Text != nil:
			texts = append(texts, *part.Text)
		case part.Kind == contract.PartKindChoice && part.Question != nil:
			question = true
			line := *part.Question
			for _, option := range part.Options {
				line += " [" + option.Label + "]"
			}
			texts = append(texts, line)
		case part.Credential != nil:
			texts = append(texts, "[secret card] "+part.Credential.Label)
		case part.Attachment != nil && part.Attachment.Kind == contract.AttachmentKindHTML:
			// The user sees the whole card; its extracted text keeps only headings.
			if data, err := os.ReadFile(filepath.Join(p.env.Dir, "polymux", "attachments", part.Attachment.ID)); err == nil {
				texts = append(texts, "[card] "+strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(string(data), " "))), " "))
			} else if part.Attachment.ExtractedText != nil {
				texts = append(texts, "[card] "+*part.Attachment.ExtractedText)
			}
		}
	}
	return strings.Join(texts, "\n"), question
}

func (p *Polymux) Close() {
	if p.http != nil {
		p.http.Close()
	}
	if p.api != nil {
		_ = p.api.Close()
	}
	if p.runtime != nil {
		_ = p.runtime.Close()
	}
	if p.release != nil {
		_ = p.release()
	}
	if p.db != nil {
		_ = p.db.Close()
	}
}

// Null never replies. Every task should fail against it, so a pass means a check is too lenient.
type Null struct{}

func (Null) Name() string                     { return "null" }
func (Null) Start(context.Context, Env) error { return nil }
func (Null) Send(string, string) error        { return nil }
func (Null) Event(string) error               { return nil }
func (Null) Seen() ([]Message, error)         { return nil, nil }
func (Null) Close()                           {}

func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
