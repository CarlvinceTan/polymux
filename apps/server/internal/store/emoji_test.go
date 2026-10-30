package store

import (
	"strings"
	"testing"
)

func TestValidReaction(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, emoji string
		valid       bool
	}{
		{name: "default", emoji: "👍", valid: true},
		{name: "heart", emoji: "❤️", valid: true},
		{name: "punctuation", emoji: "‼️", valid: true},
		{name: "custom", emoji: "🦁", valid: true},
		{name: "skin tone", emoji: "👍🏽", valid: true},
		{name: "joined", emoji: "👩🏽‍💻", valid: true},
		{name: "family", emoji: "👨‍👩‍👧‍👦", valid: true},
		{name: "flag", emoji: "🇦🇺", valid: true},
		{name: "tag flag", emoji: "🏴\U000e0067\U000e0062\U000e0065\U000e006e\U000e0067\U000e007f", valid: true},
		{name: "keycap", emoji: "1️⃣", valid: true},
		{name: "empty"},
		{name: "text", emoji: "hello"},
		{name: "letter", emoji: "a"},
		{name: "digit", emoji: "1"},
		{name: "symbol", emoji: "$"},
		{name: "multiple", emoji: "👍👍"},
		{name: "whitespace", emoji: "👍 "},
		{name: "control", emoji: "👍\n"},
		{name: "combining text", emoji: "👍\u0301"},
		{name: "invalid utf8", emoji: "\xff"},
		{name: "oversize", emoji: "👍" + strings.Repeat("\ufe0f", 50)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := validReaction(test.emoji); got != test.valid {
				t.Fatalf("validReaction(%q) = %v, want %v", test.emoji, got, test.valid)
			}
		})
	}
}
