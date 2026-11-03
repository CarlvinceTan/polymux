package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

const (
	quiet          = 15.0 // seconds of no activity that end a run
	defaultTimeout = 8 * time.Minute
	maxReplies     = 6
)

// Row is one run's outcome.
type Row struct {
	System     string    `json:"system"`
	Scenario   string    `json:"scenario"`
	Category   string    `json:"category"`
	Suite      string    `json:"suite"`
	Tier       int       `json:"tier"`
	Rep        int       `json:"rep"`
	Tasks      []Task    `json:"tasks"`
	Replies    []float64 `json:"replies"` // per user message, seconds to the next thing the user saw; negative when nothing came
	Hygiene    []string  `json:"hygiene,omitempty"`
	Stacked    bool      `json:"stacked,omitempty"`
	Calls      int       `json:"calls"`
	Prompt     int       `json:"prompt"`
	Cached     int       `json:"cached"`
	Completion int       `json:"completion"`
	Cost       float64   `json:"cost"`
	Messages   int       `json:"messages"`
	Duration   float64   `json:"duration"`
	TimedOut   bool      `json:"timedOut,omitempty"`
	Invalid    bool      `json:"invalid,omitempty"` // the provider failed calls, so the run isn't scored
	Error      string    `json:"error,omitempty"`
}

// Suite runs scenarios against systems through one proxy.
type Suite struct {
	Proxy   *Proxy
	Out     string
	Systems map[string]func() System
	Judge   *Judge // rules on replies a check's pattern misses; nil uses patterns alone
	Log     func(string, ...any)

	mu sync.Mutex
}

// Run plays one scenario against one system.
func (s *Suite) Run(ctx context.Context, system string, sc *Scenario, rep int) Row {
	id := fmt.Sprintf("%s-%s-%d", system, sc.Name, rep)
	dir := filepath.Join(s.Out, "runs", id)
	row := Row{System: system, Scenario: sc.Name, Category: sc.Category, Suite: sc.SuiteName(), Tier: sc.TierLevel(), Rep: rep, Stacked: sc.Stacked}
	start := time.Now()
	world, err := NewWorld(dir, start)
	if err == nil {
		err = world.Start()
	}
	if err != nil {
		row.Error = err.Error()
		return row
	}
	defer world.Close()
	if sc.Setup != nil {
		world.Life(sc.Setup)
	}
	sys := s.Systems[system]()
	defer sys.Close()
	if err := sys.Start(ctx, Env{Dir: dir, Model: s.Proxy.Model, ModelURL: s.Proxy.Base(id), World: world, Start: start}); err != nil {
		row.Error = err.Error()
		return row
	}
	view := &View{World: world, judge: s.Judge, memo: map[string]int{}}
	steps := sc.Steps(view)
	timeout := sc.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	type reply struct {
		due           float64
		channel, text string
	}
	var pending []reply
	var problems []string
	known := map[string]bool{}
	next, sent, last, calls, tools, dumped := 0, 0, 0.0, 0, 0, -100.0
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		now := time.Since(start).Seconds()
		for next < len(steps) && steps[next].At <= now {
			step := steps[next]
			next++
			channel := step.Channel
			if channel == "" {
				channel = "main"
			}
			switch {
			case step.Mail != nil:
				world.Mail(*step.Mail)
				if step.Notify {
					err = sys.Event(fmt.Sprintf("New email from %s: %s", step.Mail.From, step.Mail.Subject))
				}
			case step.Event:
				err = sys.Event(step.Text)
			default:
				err = sys.Send(channel, step.Text)
				view.Sent = append(view.Sent, Sent{At: now, Channel: channel, Text: step.Text})
			}
			if err != nil {
				problems = append(problems, err.Error())
			}
			last = now
		}
		kept := pending[:0]
		for _, r := range pending {
			if r.due > now {
				kept = append(kept, r)
				continue
			}
			if err := sys.Send(r.channel, r.text); err != nil {
				problems = append(problems, err.Error())
			}
			view.Sent = append(view.Sent, Sent{At: now, Channel: r.channel, Text: r.text})
			last = now
		}
		pending = kept
		if seen, err := sys.Seen(); err == nil {
			sort.SliceStable(seen, func(i, j int) bool { return seen[i].At < seen[j].At })
			view.Seen = seen
			replied := false
			for _, m := range seen {
				key := fmt.Sprintf("%s|%.3f|%s", m.Channel, m.At, m.Text)
				if known[key] {
					continue
				}
				known[key], last = true, now
				// A person answers a burst of messages once.
				if text := s.reply(sc, view, m); text != "" && sent < maxReplies && !replied {
					sent, replied = sent+1, true
					pending = append(pending, reply{now + 3, m.Channel, text})
				}
			}
		}
		if c, t := len(s.Proxy.Calls(id)), len(world.Calls()); c != calls || t != tools {
			calls, tools, last = c, t, now
		}
		busy := s.Proxy.Busy(id) || world.Busy()
		if b, ok := sys.(interface{ Busy() bool }); ok && b.Busy() {
			busy = true
			if now-last >= 8 && now-dumped >= 30 {
				// A turn that does nothing for this long is stuck somewhere worth seeing.
				dumped = now
				stall(dir, now)
			}
		}
		if next == len(steps) && len(pending) == 0 && !busy && now >= sc.Settle && now-last >= quiet {
			break
		}
		if now >= timeout.Seconds() {
			row.TimedOut = true
			break
		}
		select {
		case <-ctx.Done():
			row.Error = ctx.Err().Error()
			return row
		case <-ticker.C:
		}
	}
	row.Duration = time.Since(start).Seconds()
	if r, ok := sys.(interface{ Routines() []contract.Routine }); ok {
		view.Routines = r.Routines()
	}
	row.Tasks = sc.Check(view)
	row.Replies = replies(view)
	row.Hygiene = hygiene(view.Seen)
	row.Messages = len(view.Seen)
	for _, c := range s.Proxy.Calls(id) {
		row.Calls++
		row.Prompt += c.Prompt
		row.Cached += c.Cached
		row.Completion += c.Completion
		row.Cost += c.Cost
	}
	if failed := s.Proxy.Failed(id); failed > 0 {
		problems = append(problems, fmt.Sprintf("provider refused or failed %d model calls", failed))
		row.Invalid = true
	}
	if len(problems) > 0 {
		row.Error = strings.Join(problems[:min(3, len(problems))], "; ")
	}
	s.record(dir, row, view)
	return row
}

// stall writes every goroutine's stack, for runs whose system is busy but idle.
func stall(dir string, at float64) {
	f, err := os.Create(filepath.Join(dir, fmt.Sprintf("stall-%03.0f.txt", at)))
	if err != nil {
		return
	}
	defer f.Close()
	_ = pprof.Lookup("goroutine").WriteTo(f, 2)
}

func (s *Suite) reply(sc *Scenario, v *View, m Message) string {
	if sc.Silent {
		return ""
	}
	if sc.Reply != nil {
		if text := sc.Reply(v, m); text != "" {
			return text
		}
	}
	return defaultReply(sc, v, m)
}

// replies times each user message to the next thing the user saw.
func replies(v *View) []float64 {
	out := make([]float64, 0, len(v.Sent))
	for _, s := range v.Sent {
		latency := -1.0
		for _, m := range v.Seen {
			if m.At >= s.At && m.At-s.At <= 120 {
				latency = m.At - s.At
				break
			}
		}
		out = append(out, latency)
	}
	return out
}

var (
	plumbing = regexp.MustCompile(`\b(web_search|fetch_page|reading_shelf|search_restaurants|book_table|cancel_booking|exchange_rate|export_photos|list_files|read_file|write_file|run_tests|email_search|email_read|email_send|calendar_list|calendar_create|calendar_update|calendar_delete|bank_balance|bank_transactions|bank_upcoming|browser_open|browser_submit|send_to_user|NO_RESPONSE|manage_tasks|create_task|track_work|tool call)\b`)
	filler   = regexp.MustCompile(`(?i)(^|\n)\s*(great question|certainly!|absolutely!|of course!)|i hope this helps|let me know if you have any (other |more )?questions`)
)

// hygiene lists plumbing and filler the user saw.
func hygiene(seen []Message) []string {
	var out []string
	for _, m := range seen {
		if found := plumbing.FindString(m.Text); found != "" {
			out = append(out, "plumbing: "+found)
		}
		if found := filler.FindString(m.Text); found != "" {
			out = append(out, "filler: "+strings.TrimSpace(found))
		}
	}
	return out
}

func (s *Suite) record(dir string, row Row, v *View) {
	detail, _ := json.MarshalIndent(map[string]any{"row": row, "sent": v.Sent, "seen": v.Seen, "tools": v.World.Calls(),
		"bookings": v.World.Bookings(), "state": v.World.State(), "routines": v.Routines, "rulings": v.Rulings, "requests": v.World.Requests(), "calls": s.Proxy.Calls(fmt.Sprintf("%s-%s-%d", row.System, row.Scenario, row.Rep))}, "", " ")
	_ = os.WriteFile(filepath.Join(dir, "run.json"), detail, 0o600)
	line, _ := json.Marshal(row)
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.Out, "runs.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}
