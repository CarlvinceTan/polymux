package bench

import (
	"regexp"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// Step is a scripted user message, outside event or arriving email.
type Step struct {
	At      float64 // seconds after the run starts
	Text    string
	Event   bool
	Channel string // "main" by default; "group" is the group chat where a system has one
	Mail    *Email // lands in the persona's inbox
	Notify  bool   // and the system hears of it, as an email integration's push would tell it
}

// Task is one thing the user asked for, judged when the run ends.
type Task struct {
	Name   string  `json:"name"`
	OK     bool    `json:"ok"`
	Asked  float64 `json:"asked"`  // when it was asked; negative when not timed
	Done   float64 `json:"done"`   // when the result reached the user; negative when it didn't
	Target float64 `json:"target"` // time-to-completion target in seconds
}

// Scenario is a scripted situation and how its outcome is judged.
type Scenario struct {
	Name     string
	Category string
	Suite    string // "solo" by default, or "group" for scenarios about several bots
	Tier     int    // difficulty, 1 by default
	Setup    func(l *life)
	Steps    func(v *View) []Step
	// Reply is the user's answer to a message, or "" for none. Nil uses the default user.
	Reply   func(v *View, m Message) string
	Check   func(v *View) []Task
	Timeout time.Duration
	Settle  float64 // earliest end in seconds, for work scheduled ahead such as reminders
	Stacked bool    // counts towards throughput
	Booking bool    // the default user answers the booking question
	Silent  bool    // the user sends nothing beyond the script
}

// Sent is a message the simulated user sent.
type Sent struct {
	At      float64 `json:"at"`
	Channel string  `json:"channel"`
	Text    string  `json:"text"`
}

// View is what the simulated user and the checks can see.
type View struct {
	World    *World
	Seen     []Message
	Sent     []Sent
	Routines []contract.Routine // the system's scheduled work when the run ended
	Rulings  []Ruling           // what the judge decided, for review
	judge    *Judge
	memo     map[string]int
}

// denies reports whether the judge finds the replies from asked don't do what question describes, for checks that
// look for a mistake: a pattern can spot a suspicious phrase but not whether it was said or ruled out. Without a judge
// the phrase alone counts.
func (v *View) denies(asked float64, question string) bool {
	if v.judge == nil {
		return false
	}
	var text strings.Builder
	for _, m := range v.After(asked) {
		text.WriteString(m.Text + "\n")
	}
	verdict, err := v.judge.Ask(question, text.String())
	ruling := Ruling{Question: question, Verdict: verdict}
	if err != nil {
		ruling.Error = err.Error()
	}
	v.Rulings = append(v.Rulings, ruling)
	return err == nil && !verdict
}

// claims is when the replies from asked on first passed test or, failing that, when the judge finds they answer
// question; the judge's answer counts from the last of those replies.
func (v *View) claims(asked float64, question string, test func(string) bool) float64 {
	if at := v.Joined(asked, test); at >= 0 || v.judge == nil {
		return at
	}
	var text strings.Builder
	last := -1.0
	for _, m := range v.After(asked) {
		text.WriteString(m.Text + "\n")
		last = m.At
	}
	if last < 0 {
		return -1
	}
	verdict, err := v.judge.Ask(question, text.String())
	ruling := Ruling{Question: question, Verdict: verdict}
	if err != nil {
		ruling.Error = err.Error()
	}
	v.Rulings = append(v.Rulings, ruling)
	if !verdict {
		return -1
	}
	return last
}

// SuiteName is the suite the scenario belongs to.
func (s *Scenario) SuiteName() string {
	if s.Suite == "" {
		return "solo"
	}
	return s.Suite
}

// TierLevel is the scenario's difficulty tier.
func (s *Scenario) TierLevel() int { return max(s.Tier, 1) }

func (v *View) flag(key string) bool { return v.memo[key] > 0 }
func (v *View) set(key string)       { v.memo[key]++ }

// SentAt is when the user first sent a message containing needle, or fallback.
func (v *View) SentAt(needle string, fallback float64) float64 {
	for _, s := range v.Sent {
		if strings.Contains(s.Text, needle) {
			return s.At
		}
	}
	return fallback
}

// After returns what the user saw from time t on.
func (v *View) After(t float64) []Message {
	var out []Message
	for _, m := range v.Seen {
		if m.At >= t {
			out = append(out, m)
		}
	}
	return out
}

// First is when a single message from time t on first passed test, or -1.
func (v *View) First(t float64, test func(string) bool) float64 {
	for _, m := range v.After(t) {
		if test(m.Text) {
			return m.At
		}
	}
	return -1
}

// Joined is when everything seen from time t on first passed test together, or -1.
// A result can arrive over several messages and cards.
func (v *View) Joined(t float64, test func(string) bool) float64 {
	var text strings.Builder
	for _, m := range v.After(t) {
		text.WriteString(" " + m.Text)
		if test(text.String()) {
			return m.At
		}
	}
	return -1
}

// task judges a result that must reach the user after `after`, timed from `asked`.
func (v *View) task(name string, asked, after, target float64, test func(string) bool) Task {
	done := v.Joined(after, test)
	return Task{Name: name, OK: done >= 0, Asked: asked, Done: done, Target: target}
}

func (v *View) calls(names ...string) []ToolCall {
	var out []ToolCall
	for _, c := range v.World.Calls() {
		for _, n := range names {
			if c.Name == n {
				out = append(out, c)
			}
		}
	}
	return out
}

var invite = regexp.MustCompile(`(?i)say the word|let me know|which (one|would)|your call|pick one|tell me which`)

var offer = regexp.MustCompile(`(?i)\b(want me to|should i|would you like|shall i|do you want|let me know if|happy to)\b`)

// defaultReply is a user who answers the booking question when asked, ignores offers,
// and answers at most two other questions without adding requirements.
func defaultReply(s *Scenario, v *View, m Message) string {
	low := strings.ToLower(m.Text)
	options := strings.Contains(low, "bella") && strings.Contains(low, "trattoria")
	asks := m.Question || strings.Contains(m.Text, "?") || invite.MatchString(m.Text)
	if s.Booking && !v.flag("answered") && (options || (strings.Contains(low, "bella") || strings.Contains(low, "trattoria")) && asks) {
		v.set("answered")
		return bookingAnswer
	}
	if s.Booking && (strings.Contains(low, "bella") || strings.Contains(low, "trattoria")) {
		return ""
	}
	if (m.Question || strings.HasSuffix(strings.TrimSpace(m.Text), "?")) && !offer.MatchString(m.Text) && v.memo["defaults"] < 2 {
		v.set("defaults")
		return "No preference, use your best judgement."
	}
	return ""
}

const bookingAnswer = "Bella Cucina at 8:30pm please."
