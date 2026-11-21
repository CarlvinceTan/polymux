package server

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/CarlvinceTan/polymux/apps/server/internal/chatstyle"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

const (
	maxBubbleWords = 45
	maxBubbles     = 2
	openerWindow   = 12
)

// pace bounces a reply that breaks the chat's bubble rules: a wall of text,
// more than two bubbles since the last real work, or a parroted opener.
// Bounces are guidance the model can act on, so wall and parrot bounce once
// per burst and turn; the cap always holds. state.mu is held.
func (t *chatTool) pace(ctx context.Context, state *turnState, beats []contract.Part) error {
	if !state.wallBounced {
		for _, beat := range beats {
			if beat.Text == nil || strings.Contains(*beat.Text, "```") {
				continue
			}
			if n := len(strings.Fields(*beat.Text)); n > maxBubbleWords {
				state.wallBounced = true
				return fmt.Errorf("a %d-word bubble is a wall of text for chat. Say less, or put a blank line between separate ideas", n)
			}
		}
	}
	if state.bubbles+len(beats) > maxBubbles {
		return fmt.Errorf("at most two bubbles per reply: keep a result and its caveat together, and give a second bubble only to a genuinely separate idea")
	}
	if state.parrotBounced {
		return nil
	}
	var text string
	for _, beat := range beats {
		if beat.Text != nil {
			text = *beat.Text
			break
		}
	}
	o := opener(text)
	if o == "" {
		return nil
	}
	prev := t.openers(ctx, state)
	streak, count := 0, 0
	for i := len(prev) - 1; i >= 0 && prev[i] == o; i-- {
		streak++
	}
	for _, p := range prev {
		if p == o {
			count++
		}
	}
	if streak >= 2 || count >= openerWindow/3 {
		state.parrotBounced = true
		return fmt.Errorf("your recent messages keep opening with %q; vary the opener, or react instead if it's just an ack", o)
	}
	return nil
}

// style bounces a message to the user that reads like an assistant wrote it, once per turn: the bounce is
// guidance, so a retry goes through. A group message that was already posted is a replay and isn't checked.
func (t *chatTool) style(ctx context.Context, state *turnState, group bool, base string, beats []contract.Part, question string) error {
	if state.styleBounced {
		return nil
	}
	if group {
		id := base
		if len(beats) > 1 {
			id += ":0"
		}
		if _, err := t.store.Message(ctx, id); err == nil {
			return nil
		}
	}
	texts := []string{question}
	for _, beat := range beats {
		if beat.Text != nil {
			texts = append(texts, *beat.Text)
		}
	}
	for _, text := range texts {
		if label, fix := chatstyle.Tell(text); label != "" {
			state.styleBounced = true
			return fmt.Errorf("that has %s, which reads like an assistant rather than a person texting: %s, then send it again", label, fix)
		}
	}
	return nil
}

// opener is a message's first three words, lowercased and stripped of punctuation.
func opener(text string) string {
	if strings.Contains(text, "```") {
		return ""
	}
	words := strings.Fields(strings.ToLower(text))
	if len(words) > 3 {
		words = words[:3]
	}
	var out []string
	for _, w := range words {
		if w = strings.TrimFunc(w, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
		}); w != "" {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

// openers lists this bot's recent message openers in the chat, oldest first.
func (t *chatTool) openers(ctx context.Context, state *turnState) []string {
	messages, err := t.store.Messages(ctx, state.run.ConversationID, 0, 4*openerWindow)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range messages {
		if m.Role != contract.MessageRoleAssistant || value(m.AgentID) != state.run.AgentID {
			continue
		}
		if o := opener(store.Text(m)); o != "" {
			out = append(out, o)
			if len(out) == openerWindow {
				break
			}
		}
	}
	return out
}
