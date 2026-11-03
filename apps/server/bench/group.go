package bench

import (
	"regexp"
	"slices"
	"strings"
	"time"
)

// The group suite: jobs for a team of specialist bots sharing a chat with the user, in the style of Grok Bot's
// groups. The same jobs run on single-bot systems in their main chat, so the index shows whether splitting the
// work between specialists helps or confuses.

func group(at float64, text string) Step { return Step{At: at, Text: text, Channel: "group"} }

// voices lists the bots that wrote between two times.
func (v *View) voices(from, to float64, test func(string) bool) []string {
	var out []string
	for _, m := range v.Seen {
		// A handoff notice passes the turn on; it isn't an answer.
		if m.At >= from && m.At < to && m.From != "" && !strings.HasPrefix(m.Text, "Handed off to") && test(m.Text) && !slices.Contains(out, m.From) {
			out = append(out, m.From)
		}
	}
	return out
}

var (
	yes     = regexp.MustCompile(`(?i)\byes\b|can afford|affordable|well under|comfortably|plenty of room|within (your |the )?budget|fits|covers|keeps you under|under the cap|255\.6`)
	dollars = regexp.MustCompile(`\$\d`)
	anyText = func(string) bool { return true }
)

var groupScenarios = []*Scenario{
	// The browser is already signed in to the portal, so this tests routing, not logins (exam-plan tests those).
	{Name: "team-routing", Suite: "group", Category: "coordination", Tier: 2, Setup: func(l *life) { l.cobalt = true },
		Steps: static(group(0, "How much did I spend eating out last month?"), group(50, "Is there an Italian place in the West Village with a table for 4 this Friday?"),
			group(100, "What's due next for my stats class?")),
		Check: func(v *View) []Task {
			var due time.Time
			v.state(func(l *life) { due = l.d.weekday(1, time.Wednesday) })
			spend := v.Joined(0, func(t string) bool { return strings.Contains(strings.ReplaceAll(t, ",", ""), "383.4") })
			table := v.Joined(50, func(t string) bool { return strings.Contains(t, "Bella") || strings.Contains(t, "Trattoria") })
			next := v.Joined(100, func(t string) bool {
				return regexp.MustCompile(`(?i)problem set 5|ps ?5`).MatchString(t) && (strings.Contains(t, due.Weekday().String()) || strings.Contains(t, due.Format("Jan 2")) || strings.Contains(t, due.Format("2 Jan")))
			})
			owners := []struct {
				from, to float64
				who      string
			}{{0, 50, "Finn"}, {50, 100, "Theo"}, {100, 1e9, "Iris"}}
			focused := true
			for _, o := range owners {
				for _, who := range v.voices(o.from, o.to, anyText) {
					focused = focused && who == o.who
				}
			}
			return []Task{done("spending-answered", spend >= 0, 0, spend, 60), done("table-answered", table >= 0, 50, table, 60),
				done("deadline-answered", next >= 0, 100, next, 90), check("only-the-owner-answers", focused && spend >= 0 && table >= 0 && next >= 0)}
		}},

	{Name: "team-booking", Suite: "group", Category: "coordination", Tier: 2,
		Steps: static(group(0, "Book a table for 4 this Friday at 8:30pm at an Italian place in the West Village — Bella Cucina if they have it.")),
		Check: func(v *View) []Task {
			var active []Booking
			for _, b := range v.World.Bookings() {
				if b.Status == "active" {
					active = append(active, b)
				}
			}
			right := len(active) == 1 && strings.Contains(strings.ToLower(active[0].Restaurant), "bella") && halfEight.MatchString(active[0].Time) && active[0].Party == 4
			told := v.Joined(0, func(t string) bool { return regexp.MustCompile(`(?i)booked|confirm|BK\d`).MatchString(t) })
			claimed := v.voices(0, 1e9, regexp.MustCompile(`(?i)booked|confirm|BK\d`).MatchString)
			return []Task{done("one-booking", right, 0, told, 60), check("one-voice-confirms", right && len(claimed) <= 1), check("brief", right && len(v.Seen) <= 4)}
		}},

	{Name: "team-trip", Suite: "group", Category: "coordination", Tier: 3, Timeout: 10 * time.Minute, Reply: approve("Yes, go ahead."),
		Steps: func(v *View) []Step {
			return []Step{group(0, "I'm in Lisbon for work "+v.World.life.d.trip()+". Book me one evening activity on Wayfarer, rated 9 or more, that doesn't clash with my meetings, and tell me if I can afford it while keeping this month's eating out and entertainment under $500.")}
		},
		Check: func(v *View) []Task {
			var trips []Trip
			fits := false
			v.state(func(l *life) {
				for _, t := range l.trips {
					if !t.Gone {
						trips = append(trips, t)
					}
				}
				if len(trips) != 1 {
					return
				}
				var a *Activity
				for n := range activities {
					if activities[n].ID == trips[0].Activity {
						a = &activities[n]
					}
				}
				start, err := time.ParseInLocation("2006-01-02 15:04", trips[0].Date+" "+trips[0].Time, lisbonZone)
				end := start.Add(time.Duration(a.Hours * float64(time.Hour)))
				fits = err == nil && a.Rating >= 9 && start.Hour() >= 17 && l.clashBefore(start, end) == nil
			})
			verdict := v.claims(0, "Does the assistant tell the user whether the booked activity keeps this month's eating out and entertainment under $500, with the dollar figures?",
				func(t string) bool { return yes.MatchString(t) && dollars.MatchString(t) })
			return []Task{done("evening-activity-booked", fits, 0, verdict, 240), done("affordability-answered", verdict >= 0, 0, verdict, 240),
				check("no-duplicate-bookings", len(trips) == 1), check("brief", fits && len(v.Seen) <= 8)}
		}},

	{Name: "ask-finn", Suite: "group", Category: "coordination", Tier: 2,
		Steps: static(say(0, "Can I afford a $1,200 laptop this month and still keep at least $2,000 in checking?")),
		Check: func(v *View) []Task {
			no := regexp.MustCompile(`(?i)\bno\b|can.t|cannot|won.t|short|not quite|fall below|dip below|drop below|1,?947`)
			naive := regexp.MustCompile(`(?i)\byes\b,? you can|you can afford it|you('d| would) still have \$?2,?120`)
			verdict := v.claims(0, "Does the assistant tell the user they can't buy a $1,200 laptop this month and still keep at least $2,000 in checking, once the bills scheduled before month end are counted?",
				func(t string) bool { return no.MatchString(t) && !naive.MatchString(t) })
			bills := v.Joined(0, func(t string) bool {
				return regexp.MustCompile(`(?i)1,?947|bills?\b|scheduled|upcoming|verizon|coned|streamly`).MatchString(t)
			})
			return []Task{done("says-no", verdict >= 0, 0, verdict, 60), check("counts-upcoming-bills", bills >= 0),
				check("checked-the-bank", len(v.calls("bank_balance", "bank_transactions", "bank_upcoming")) > 0)}
		}},

	{Name: "team-burst", Suite: "group", Category: "coordination", Tier: 3, Stacked: true, Booking: true,
		Steps: static(group(0, "What did I spend on groceries last month?"), group(8, "Book Bella Cucina for 2 this Saturday at 7pm."),
			group(16, "And put a STAT 201 study block in my calendar tomorrow, 7 to 8pm.")),
		Check: func(v *View) []Task {
			var block bool
			v.state(func(l *life) {
				blocks := l.added(study.MatchString)
				block = len(blocks) == 1 && blocks[0].Start.Equal(at(l.d.day(1), 19, 0)) && blocks[0].End.Equal(at(l.d.day(1), 20, 0))
			})
			var active []Booking
			for _, b := range v.World.Bookings() {
				if b.Status == "active" {
					active = append(active, b)
				}
			}
			seven := regexp.MustCompile(`(^|[^0-9])(7|19)(:00)?($|[^0-9:])`)
			booked := len(active) == 1 && strings.Contains(strings.ToLower(active[0].Restaurant), "bella") && seven.MatchString(active[0].Time) && active[0].Party == 2
			groceries := v.Joined(0, func(t string) bool { return strings.Contains(strings.ReplaceAll(t, ",", ""), "289.45") })
			bookedAt := v.Joined(8, func(t string) bool { return regexp.MustCompile(`(?i)booked|BK\d`).MatchString(t) })
			blockAt := v.Joined(16, func(t string) bool { return strings.Contains(t, "7") && study.MatchString(t) })
			return []Task{done("groceries-answered", groceries >= 0, 0, groceries, 60), done("dinner-booked", booked, 8, bookedAt, 90),
				done("study-block-added", block, 16, blockAt, 90), check("settles-quickly", booked && block && len(v.Seen) <= 9 && len(v.voices(0, 1e9, anyText)) <= 3)}
		}},

	{Name: "team-handoff", Suite: "group", Category: "coordination", Tier: 2, Booking: true,
		Steps: static(group(0, "@Theo @Finn between you, work out whether I can comfortably afford a $150 dinner this Saturday, and if so book an Italian place in the West Village for 2 at 8:30pm. Then let me know.")),
		Check: func(v *View) []Task {
			var active []Booking
			for _, b := range v.World.Bookings() {
				if b.Status == "active" {
					active = append(active, b)
				}
			}
			right := len(active) == 1 && halfEight.MatchString(active[0].Time) && active[0].Party == 2
			told := v.Joined(0, func(t string) bool { return regexp.MustCompile(`(?i)booked|BK\d`).MatchString(t) })
			return []Task{done("booked-once", right, 0, told, 120), check("settles-quickly", right && len(v.Seen) <= 6)}
		}},
}

// All lists every scenario: tier 1 first, then the solo and group suites' harder ones.
func All() []*Scenario {
	out := slices.Clone(Scenarios)
	out = append(out, soloScenarios...)
	return append(out, groupScenarios...)
}
