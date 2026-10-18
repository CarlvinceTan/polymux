package evals

import (
	"fmt"
	"strings"
	"testing"
)

// summaryKeepingFacts copies a Facts section and drops every other summarizer line.
func summaryKeepingFacts(req *ChatRequest) Response {
	text := requestText(req)
	if !strings.Contains(text, "You are a context summarization assistant") {
		return TextReply("noted")
	}
	if section, ok := m4Facts(text); ok {
		return TextReply(section)
	}
	return TextReply("Compacted summary without pinned facts.")
}

func m4Facts(text string) (string, bool) {
	at := strings.LastIndex(text, "\nFacts:\n")
	if at < 0 {
		return "", false
	}
	var b strings.Builder
	for i, line := range strings.Split(text[at+1:], "\n") {
		if i > 0 && !strings.HasPrefix(line, "- ") {
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	if !strings.Contains(b.String(), "\n- ") {
		return "", false
	}
	return b.String(), true
}

func TestM4(t *testing.T) {
	steps := make([]Step, 400)
	for i := range steps {
		steps[i] = Step{Response: summaryKeepingFacts}
	}
	h := Start(t, Options{ID: "M4", Steps: steps})
	seq := 0
	if !m4Send(t, h, &seq, m4Plant()) || !m4Until(t, h, &seq, 1) || !m4Kept(t, h) {
		return
	}
	h.Restart()
	if !m4Until(t, h, &seq, 2) || !m4Kept(t, h) || !m4Until(t, h, &seq, 10) || !m4Kept(t, h) {
		return
	}
	if err := h.Emulator.DoneAllowExtra(); err != nil {
		fail(t, "M4", err.Error())
		return
	}
	pass(t, "M4", "flight SQ322 survived compaction and restart, and the facts block stayed within 2KB")
}

func m4Plant() string {
	var b strings.Builder
	b.WriteString("My flight number is SQ322.\n")
	for i := 2; i <= 50; i++ {
		fmt.Fprintf(&b, "fact %02d code %04d%s\n", i, 1000+i, strings.Repeat(" x", 22))
	}
	b.WriteString("M4_EARLY_PAD ")
	b.WriteString(strings.Repeat("padding ", 200))
	return b.String()
}

func m4Send(t *testing.T, h *Harness, seq *int, text string) bool {
	t.Helper()
	*seq++
	id := fmt.Sprintf("m4-%d", *seq)
	if code := h.Send(agentID, id, text); code != 202 && code != 200 {
		fail(t, "M4", fmt.Sprintf("send %s status %d", id, code))
		return false
	}
	if run := h.WaitRun(agentID, id, agentID, deadline); run.Status != "completed" {
		fail(t, "M4", fmt.Sprintf("turn %s status %s: %+v", id, run.Status, run.Error))
		return false
	}
	return true
}

func m4Until(t *testing.T, h *Harness, seq *int, want int) bool {
	t.Helper()
	for m4Compactions(h) < want {
		if *seq >= 120 {
			fail(t, "M4", fmt.Sprintf("compaction stalled at %d, want %d", m4Compactions(h), want))
			return false
		}
		pad := fmt.Sprintf("turn %04d %s", *seq, strings.Repeat("padding ", 1400))
		if !m4Send(t, h, seq, pad) {
			return false
		}
	}
	return true
}

func m4Compactions(h *Harness) int {
	n := 0
	for _, req := range h.Emulator.Requests() {
		if strings.Contains(requestText(req), "You are a context summarization assistant") {
			n++
		}
	}
	return n
}

func m4Kept(t *testing.T, h *Harness) bool {
	t.Helper()
	chat := m4LastChat(h)
	section, ok := m4Facts(chat)
	switch {
	case strings.Contains(chat, "M4_EARLY_PAD"):
		fail(t, "M4", "early message was still in the request")
	case !ok || !strings.Contains(section, "SQ322"):
		fail(t, "M4", "next request dropped SQ322")
	case len(section) > 2048 || strings.Contains(section, "code 1050"):
		fail(t, "M4", fmt.Sprintf("facts block %d bytes kept the newest fact", len(section)))
	case !m4SummariesBounded(h):
		fail(t, "M4", "summarizer facts block exceeded 2048 bytes")
	default:
		return true
	}
	return false
}

func m4SummariesBounded(h *Harness) bool {
	for _, req := range h.Emulator.Requests() {
		text := requestText(req)
		if !strings.Contains(text, "You are a context summarization assistant") {
			continue
		}
		section, ok := m4Facts(text)
		if !ok || len(section) > 2048 {
			return false
		}
	}
	return true
}

func m4LastChat(h *Harness) string {
	reqs := h.Emulator.Requests()
	for i := len(reqs) - 1; i >= 0; i-- {
		text := requestText(reqs[i])
		if !strings.Contains(text, "You are a context summarization assistant") {
			return text
		}
	}
	return ""
}
