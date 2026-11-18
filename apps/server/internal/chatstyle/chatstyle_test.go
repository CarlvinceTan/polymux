package chatstyle

import "testing"

func TestTellCatches(t *testing.T) {
	for _, text := range []string{
		"Booked it \u2014 Friday at 8",
		"Booked it \u2013 Friday at 8",
		"Booked it -- Friday at 8",
		"Done. I hope this helps!",
		"hope that helps",
		"Let me know if you need anything else.",
		"Let me know if there's anything I can change",
		"Feel free to ask",
		"Don't hesitate to reach out",
		"Happy to help",
		"I'd be happy to look into it",
		"Great question! It's 8 km",
		"Good question, it depends",
		"You're absolutely right, it was Thursday",
		"Certainly! It's booked",
		"Done. Of course! Anything else",
		"Let's delve into the options",
		"It's a testament to the team",
		"The bill underscores the problem",
		"It works seamlessly with your calendar",
		"I'll utilize the spare slot",
		"We can leverage the discount",
		"Additionally, the Pro is heavier",
		"Moreover it's cheaper",
		"It's not just cheaper, but lighter too",
		"Not only is it lighter but also cheaper",
	} {
		if label, fix := Tell(text); label == "" || fix == "" {
			t.Errorf("Tell(%q) found nothing", text)
		}
	}
}

func TestTellHuman(t *testing.T) {
	for _, text := range []string{
		"Done, Thursday 10am",
		"The Air honestly",
		"The Pro's screen is nicer but not $600 nicer for what you do",
		"No, they're full at 8. They have 6:30 or 9:15 if either works?",
		"Halfway, 3 of the 6 invoices are done. The Telstra one won't download so I'm trying it again",
		"haha anytime, go rest",
		"Heads up, your flight moved to 7:40 so you'd miss dinner at 7. Want me to push the booking?",
		"On it, handing it to Claude Code",
		"SQLite if it's just you on one machine, there's way less to run. Postgres once more than one thing writes at the same time, that's where SQLite starts to struggle",
		"Absolutely, it's booked",
		"Not bad, you?",
		"It's not cheap but it's worth it",
		"Let me know if 6:30 works",
		"The run's 2-3 hours",
		"Use `a -- b` in the shell",
		"```\nx := a \u2014 b // delve\n```",
	} {
		if label, _ := Tell(text); label != "" {
			t.Errorf("Tell(%q) = %q, want none", text, label)
		}
	}
}

func TestTidy(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"\u201cFine\u201d she said, it\u2019s \u2018ok\u2019", `"Fine" she said, it's 'ok'`},
		{"i think so", "i think so"},
		{"Ho comprato i libri", "Ho comprato i libri"},
		{"for i in xs, i++", "for i in xs, i++"},
		{"run `echo \u201cx\u201d` now", "run `echo \u201cx\u201d` now"},
		{"```\ns := \u201cx\u201d\n``` \u201cdone\u201d", "```\ns := \u201cx\u201d\n``` \"done\""},
		{"haha anytime, go rest", "haha anytime, go rest"},
	} {
		if got := Tidy(test.in); got != test.want {
			t.Errorf("Tidy(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}
