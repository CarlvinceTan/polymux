package bench

import (
	"strings"
	"testing"
)

func TestHygiene(t *testing.T) {
	for _, test := range []struct {
		name string
		seen []Message
		want string
	}{
		{"clean", []Message{{Text: "Done, pushed."}}, ""},
		{"wall", []Message{{Text: strings.TrimSpace(strings.Repeat("word ", 50))}}, "wall: 50 words"},
		{"code block", []Message{{Text: "Fix:\n```\n" + strings.Repeat("x y z\n", 20) + "```"}}, ""},
		{"card", []Message{{Text: "[card] " + strings.Repeat("word ", 50)}}, ""},
		{"question", []Message{{Text: strings.Repeat("word ", 50), Question: true}}, ""},
		{"parrot", []Message{{Text: "Done, pushed."}, {Text: "Done, pushed."}, {Text: "Done, pushed."}}, "parrot: done pushed"},
		{"varied", []Message{{Text: "Done, pushed."}, {Text: "All set."}, {Text: "Done, pushed."}}, ""},
		{"em dash", []Message{{Text: "Booked it — Friday at 8"}}, "style: an em dash"},
		{"assistant word", []Message{{Text: "Let's delve into the options"}}, "style: an assistant word"},
		{"filler first", []Message{{Text: "Great question! It's 8 km"}}, "filler: Great question"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := hygiene(test.seen)
			if test.want == "" {
				if len(got) != 0 {
					t.Fatalf("hygiene = %v, want none", got)
				}
				return
			}
			for _, g := range got {
				if strings.Contains(g, test.want) {
					return
				}
			}
			t.Fatalf("hygiene = %v, want %q", got, test.want)
		})
	}
}
