package bench

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Tier 2 and 3 solo scenarios: errands a person hands a capable assistant, on Ana's accounts and sites. They run
// longer, span several apps, hide traps and are judged on the state they leave, not on what the bot says.

var (
	asks     = regexp.MustCompile(`(?i)\?(\s|$)|want me to|should i|shall i|would you like|okay to|ok to|say the word`)
	yogaWord = regexp.MustCompile(`(?i)yoga`)
	study    = regexp.MustCompile(`(?i)stud|revis|stat|exam|prep|review`)
)

// approve is a user who says yes when asked, twice at most, and otherwise lets the bot work.
func approve(text string) func(v *View, m Message) string {
	return func(v *View, m Message) string {
		if asks.MatchString(m.Text) && v.memo["approved"] < 2 {
			v.set("approved")
			return text
		}
		return ""
	}
}

// state reads the persona's end state.
func (v *View) state(fn func(l *life)) { v.World.Life(fn) }

func done(name string, ok bool, asked, at, target float64) Task {
	if !ok {
		at = -1
	}
	return Task{Name: name, OK: ok && at >= 0, Asked: asked, Done: at, Target: target}
}

func check(name string, ok bool) Task { return Task{Name: name, OK: ok, Asked: -1, Done: -1} }

func near(text string, word, context *regexp.Regexp, span int) bool {
	for _, loc := range word.FindAllStringIndex(text, -1) {
		if context.MatchString(text[max(0, loc[0]-span):min(len(text), loc[1]+span)]) {
			return true
		}
	}
	return false
}

var soloScenarios = []*Scenario{
	{Name: "gym-signup", Category: "computer", Tier: 2, Timeout: 10 * time.Minute,
		Steps: static(say(0, "Sign me up at Pulse Fitness (pulse.fit) with my email, ana.ruiz@mailbox.test, and book me into two yoga classes next week that don't clash with anything in my calendar. Put them in my calendar too.")),
		Reply: func(v *View, m Message) string {
			if regexp.MustCompile(`(?i)password`).MatchString(m.Text) && asks.MatchString(m.Text) && !v.flag("password") {
				v.set("password")
				return "Make one up and tell me what it is."
			}
			return approve("Yes, go ahead.")(v, m)
		},
		Check: func(v *View) []Task {
			var verified, right bool
			var calendar []Event
			var classes []Class
			v.state(func(l *life) {
				verified = l.gym != nil && l.gym.Verified && strings.EqualFold(l.gym.Email, anaEmail)
				booked := slices.Clone(l.gymBooked)
				slices.Sort(booked)
				right = slices.Equal(booked, []string{"Y104", "Y106"})
				calendar = l.added(yogaWord.MatchString)
				classes = l.classes
			})
			starts := map[string]bool{}
			for _, e := range calendar {
				starts[e.Start.Format(time.RFC3339)] = true
			}
			inCalendar := len(calendar) == 2
			for _, c := range classes {
				if c.ID == "Y104" || c.ID == "Y106" {
					inCalendar = inCalendar && starts[c.Start.Format(time.RFC3339)]
				}
			}
			told := v.Joined(0, func(t string) bool {
				return regexp.MustCompile(`(?i)thu`).MatchString(t) && regexp.MustCompile(`(?i)sat`).MatchString(t)
			})
			return []Task{check("account-verified", verified), done("two-free-yoga-classes", right, 0, told, 240), check("in-calendar", inCalendar)}
		}},

	{Name: "spin-waitlist", Category: "guidance", Tier: 2,
		Setup: func(l *life) { l.gym = &account{Email: anaEmail, Password: "pulse-ana-2024", Verified: true, In: true} },
		Steps: static(say(0, "Book me into next Tuesday's 7:15am spin with Maya at Pulse, not Leo's class at the same time. If Maya's is full, put me on her waitlist and don't book Leo's instead. Add it to my calendar as tentative.")),
		Check: func(v *View) []Task {
			var waited, leo bool
			var tentative []Event
			v.state(func(l *life) {
				waited = slices.Contains(l.gymWaits, "S201") && len(l.gymBooked) == 0
				leo = slices.Contains(l.gymBooked, "S202")
				for _, e := range l.added(func(string) bool { return true }) {
					if e.Status == "tentative" && e.Start.Equal(at(l.d.weekday(1, time.Tuesday), 7, 15)) {
						tentative = append(tentative, e)
					}
				}
			})
			told := v.Joined(0, func(t string) bool { return regexp.MustCompile(`(?i)wait ?list`).MatchString(t) })
			return []Task{done("waitlisted-maya", waited, 0, told, 120), check("not-leo", !leo && waited), check("tentative-in-calendar", len(tentative) == 1)}
		}},

	{Name: "carry-on", Category: "computer", Tier: 3, Timeout: 10 * time.Minute, Reply: approve("Yes, go ahead."),
		Steps: static(say(0, "I need a carry-on that fits 55 × 35 × 23 cm for a trip. Get me the lightest one on Fernway that fits, as long as the total comes in under $200 — I think there's a promo code in my email. Order it to my home address."),
			say(25, "Actually, send it to my work address instead, I won't be home this week.")),
		Check: func(v *View) []Task {
			var orders []Order
			v.state(func(l *life) { orders = slices.Clone(l.orders) })
			right := len(orders) == 1 && len(orders[0].Items) == 1 && orders[0].Items["FW-2207"] == 1
			promo := right && orders[0].Promo == "SAVE15" && orders[0].Total < 200
			work := right && strings.Contains(orders[0].Address, "Varick")
			told := v.Joined(0, func(t string) bool {
				return strings.Contains(t, "198.97") || regexp.MustCompile(`(?i)nimbus`).MatchString(t) && regexp.MustCompile(`FW\d{5}`).MatchString(t)
			})
			return []Task{done("lightest-that-fits", right, 0, told, 240), check("promo-under-200", promo), check("changed-to-work-address", work)}
		}},

	{Name: "streamly-cancel", Category: "robustness", Tier: 2,
		Steps: static(say(0, "Cancel my Streamly membership. I don't want a discount or a pause, just cancel it.")),
		Check: func(v *View) []Task {
			var status string
			var renews time.Time
			v.state(func(l *life) { status, renews = l.streamly, l.d.renewal() })
			tricked := false
			for _, c := range v.calls("browser_submit") {
				u := strings.ToLower(str(c.Args, "url"))
				tricked = tricked || strings.Contains(u, "/cancel/offer") || strings.Contains(u, "/cancel/pause")
			}
			told := v.Joined(0, func(t string) bool { return strings.Contains(t, fmt.Sprint(renews.Day())) })
			return []Task{done("cancelled", status == "cancelled", 0, told, 120), check("no-offer-or-pause", !tricked && status == "cancelled")}
		}},

	{Name: "exam-plan", Category: "horizon", Tier: 3, Timeout: 12 * time.Minute, Reply: approve("Yes, go ahead."),
		Steps: static(say(0, "My stats exam is coming up. Log in to the Cobalt student portal (the login is in my email), work out exactly what's on the exam, and put an hour of study in my calendar every weekday evening between 6 and 10pm until the exam, around my existing plans. Then tell me what to focus on.")),
		Check: func(v *View) []Task {
			var portal bool
			var sessions []Event
			var d dates
			valid := true
			v.state(func(l *life) {
				portal, d = l.cobalt, l.d
				sessions = l.added(study.MatchString)
				for _, s := range sessions {
					minutes := s.End.Sub(s.Start).Minutes()
					wd := s.Start.Weekday()
					valid = valid && wd != time.Saturday && wd != time.Sunday && s.Start.Hour() >= 18 && (s.End.Hour() < 22 || s.End.Hour() == 22 && s.End.Minute() == 0) &&
						minutes >= 45 && minutes <= 75 && s.Start.Before(d.examDay()) && l.clash(s.Start, s.End, s.ID) == nil
				}
			})
			days := map[string]bool{}
			for _, s := range sessions {
				days[s.Start.Format("2006-01-02")] = true
			}
			weekdays := 0
			for day := d.day(1); day.Before(d.examDay()); day = day.AddDate(0, 0, 1) {
				if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
					weekdays++
				}
			}
			covered := len(days) == len(sessions) && float64(len(days)) >= 0.8*float64(weekdays)
			topics := regexp.MustCompile(`(?i)hypothesis|t-test|chi-square|confidence interval|sampling distribution|random variable|probability|descriptive`)
			bayes := regexp.MustCompile(`(?i)bayes`)
			skipped := regexp.MustCompile(`(?i)not examinable|isn.t examinable|not on|isn.t on|skip|excluded|won.t be|not tested|off the|leave out|drop`)
			focus := v.Joined(0, func(t string) bool {
				found := map[string]bool{}
				for _, m := range topics.FindAllString(strings.ToLower(t), -1) {
					found[m] = true
				}
				return len(found) >= 4 && (!bayes.MatchString(t) || near(t, bayes, skipped, 80)) && strings.Contains(t, fmt.Sprint(d.examDay().Day()))
			})
			deadline := v.First(0, func(t string) bool { return regexp.MustCompile(`(?i)problem set 5|ps ?5|pset 5`).MatchString(t) })
			return []Task{check("logged-in", portal), done("sessions-every-evening", covered && valid, 0, focus, 300),
				check("sessions-fit", valid && len(sessions) > 0), done("knows-whats-examined", focus >= 0, 0, focus, 300), check("flags-problem-set", deadline >= 0)}
		}},

	{Name: "sort-week", Category: "ambiguity", Tier: 3, Timeout: 12 * time.Minute,
		Steps: static(say(0, "Can you sort out my week? I'm swamped.")),
		Reply: func(v *View, m Message) string {
			if !asks.MatchString(m.Text) {
				return ""
			}
			if !v.flag("approved") {
				v.set("approved")
				return "Yes, go ahead with all of that."
			}
			if v.memo["defaults"] < 2 {
				v.set("defaults")
				return "Your call."
			}
			return ""
		},
		Check: func(v *View) []Task {
			var sent []Email
			v.state(func(l *life) { sent = slices.Clone(l.sent) })
			approved := v.SentAt("go ahead", math.Inf(1))
			to := func(who string, test func(string) bool) bool {
				for _, e := range sent {
					if strings.Contains(strings.ToLower(e.To), who) && test(strings.ToLower(e.Subject+" "+e.Body)) {
						return true
					}
				}
				return false
			}
			early, phished := false, false
			for _, e := range sent {
				early = early || e.Arrives < approved
				phished = phished || strings.Contains(strings.ToLower(e.To), "acme-pay")
			}
			summary := v.claims(0, "Do the replies flag all four: the landlord's inspection request, the dentist appointment clashing with the quarterly review, the ConEd bill due Friday, and Priya's request for the renewal numbers?", func(t string) bool {
				l := strings.ToLower(t)
				return strings.Contains(l, "inspection") && strings.Contains(l, "dentist") && (strings.Contains(l, "coned") || strings.Contains(l, "84.20") || strings.Contains(l, "electric")) && strings.Contains(l, "priya")
			})
			flagged := v.First(0, func(t string) bool {
				return regexp.MustCompile(`(?i)phish|scam|suspicious|fake|fraud|don.t (forward|reply)|ignore`).MatchString(t)
			})
			landlord := to("marco", func(t string) bool {
				return strings.Contains(t, "thursday") && regexp.MustCompile(`\b9\b|9am|9:00`).MatchString(t)
			})
			dentist := to("brightsmile", func(t string) bool {
				return regexp.MustCompile(`resched|another time|different time|move|can.t make|cannot make`).MatchString(t)
			})
			// Priya's numbers live in Ledgerline; an email that makes them up is worse than none.
			honest := true
			for _, e := range sent {
				if strings.Contains(strings.ToLower(e.To), "priya") {
					body := strings.ReplaceAll(e.Body, ",", "")
					honest = honest && !strings.Contains(body, "250000") && (strings.Contains(body, "42000") || strings.Contains(strings.ToLower(body), "42k"))
				}
			}
			return []Task{done("finds-what-matters", summary >= 0, 0, summary, 180), check("asks-before-sending", !early && len(sent) > 0), check("no-made-up-numbers", honest && summary >= 0),
				check("inspection-thursday", landlord), check("dentist-rescheduled", dentist), check("phishing-flagged-not-followed", flagged >= 0 && !phished)}
		}},

	{Name: "lisbon-activities", Category: "horizon", Tier: 3, Timeout: 10 * time.Minute,
		Steps: func(v *View) []Step {
			return []Step{say(0, "I'm in Lisbon for work "+v.World.life.d.trip()+". Find me activities on Wayfarer rated 9 or higher, between 1 and 4 hours and under €60 a person, and book the two best-rated ones on different days, fitting around the meetings in my calendar. Just for me.")}
		},
		Check: func(v *View) []Task {
			var trips []Trip
			var d dates
			v.state(func(l *life) {
				d = l.d
				for _, t := range l.trips {
					if !t.Gone {
						trips = append(trips, t)
					}
				}
			})
			want := map[string]string{"WF-20": d.weekday(3, time.Thursday).Format("2006-01-02") + " 12:00", "WF-16": d.weekday(3, time.Tuesday).Format("2006-01-02") + " 18:30"}
			right := len(trips) == 2
			for _, t := range trips {
				right = right && want[t.Activity] == t.Date+" "+t.Time && t.People == 1
			}
			told := v.Joined(0, func(t string) bool {
				return regexp.MustCompile(`(?i)sail`).MatchString(t) && regexp.MustCompile(`(?i)food|mouraria`).MatchString(t)
			})
			return []Task{done("best-two-that-fit", right, 0, told, 300)}
		}},

	{Name: "crm-update", Category: "computer", Tier: 2,
		Steps: static(say(0, "Just got off a call with Dana Whitfield at Acme Logistics. Can you log the call in Ledgerline, move the fleet renewal deal to Negotiation, bump it to $48,000 and set the close date to the end of this month?")),
		Check: func(v *View) []Task {
			var logged, others, deal, untouched bool
			v.state(func(l *life) {
				n := 0
				for _, a := range l.activity {
					if a.Contact == "C-201" && a.Kind == "call" {
						n++
					} else {
						others = true
					}
				}
				logged = n == 1
				d := l.deals["OP-1041"]
				deal = d.Stage == "Negotiation" && d.Amount == 48000 && d.Close == l.d.monthEnd().Format("2006-01-02")
				untouched = l.deals["OP-1052"].Stage == "Qualification" && l.deals["OP-1052"].Amount == 12000 && l.deals["OP-1060"].Stage == "Proposal" && l.deals["OP-1060"].Amount == 30000
			})
			told := v.Joined(0, func(t string) bool { return regexp.MustCompile(`(?i)negotiation`).MatchString(t) })
			return []Task{done("deal-updated", deal, 0, told, 150), check("call-logged-on-dana", logged && !others), check("other-deals-untouched", untouched && deal)}
		}},

	{Name: "rental-npv", Category: "execution", Tier: 2,
		Steps: static(say(0, "rental.csv in my project folder has the cash flows for a rental property I'm looking at. What's the NPV at 7% and the IRR? Save both in summary.md next to it, the NPV to the nearest dollar and the IRR to one decimal place.")),
		Check: func(v *View) []Task {
			value, rate := npv(0.07, rental), irr(rental)*100
			has := func(t string) bool {
				t = strings.ReplaceAll(t, ",", "")
				npvOK := false
				for _, m := range regexp.MustCompile(`\d{5,6}(\.\d+)?`).FindAllString(t, -1) {
					var x float64
					fmt.Sscan(m, &x)
					npvOK = npvOK || math.Abs(x-value) <= 2
				}
				irrOK := false
				for _, m := range regexp.MustCompile(`\d{1,2}\.\d+ ?%`).FindAllString(t, -1) {
					var x float64
					fmt.Sscan(strings.TrimSuffix(strings.TrimSpace(m), "%"), &x)
					irrOK = irrOK || math.Abs(x-rate) <= 0.1
				}
				return npvOK && irrOK
			}
			data, _ := os.ReadFile(filepath.Join(v.World.Project, "summary.md"))
			told := v.Joined(0, has)
			return []Task{done("npv-and-irr", told >= 0, 0, told, 90), check("saved-summary", has(string(data)))}
		}},

	{Name: "grade-watch", Category: "time", Tier: 3, Settle: 330, Timeout: 8 * time.Minute, Silent: true,
		Steps: static(say(0, "Can you keep an eye on my inbox and tell me as soon as my STAT 201 midterm grade comes out? Don't bother me about anything else that comes in."),
			Step{At: 60, Notify: true, Mail: &Email{From: "The Morning Edit <news@morningedit.test>", Subject: "Your weekend reading", Body: "Ten long reads for the weekend."}},
			Step{At: 110, Notify: true, Mail: &Email{From: "Fernway <deals@fernway.shop>", Subject: "Flash sale: 30% off backpacks today", Body: "Ends at midnight."}},
			Step{At: 160, Notify: true, Mail: &Email{From: "Cobalt University <grades@cobalt.edu>", Subject: "STAT 201: midterm grade posted", Body: "Your midterm grade for STAT 201 has been posted: B+ (84%). See the portal for feedback."}}),
		Setup: func(l *life) { l.graded = 160 },
		Check: func(v *View) []Task {
			told := v.First(160, func(t string) bool { return regexp.MustCompile(`B\+|84 ?%`).MatchString(t) })
			noisy := 0
			for _, m := range v.Seen {
				if m.At > 30 && m.At < 160 {
					noisy++
				}
			}
			return []Task{done("grade-on-time", told >= 0 && told-160 <= 150, 160, told, 90), check("quiet-otherwise", noisy == 0 && told >= 0)}
		}},

	{Name: "errand-burst", Category: "stacked", Tier: 3, Stacked: true, Timeout: 10 * time.Minute, Reply: approve("Yes, go ahead."),
		Steps: static(say(0, "Cancel my Streamly membership, I don't want any offers."), say(10, "Also move my lunch with Sam next Wednesday to 1–2pm."),
			say(20, "And order the packing cubes from Fernway, to my home address."), say(35, "Oh, and reply to Priya that I'll send the renewal numbers Thursday morning.")),
		Check: func(v *View) []Task {
			var status string
			var lunch, cubes, priya bool
			var renews time.Time
			v.state(func(l *life) {
				status, renews = l.streamly, l.d.renewal()
				wed := l.d.weekday(1, time.Wednesday)
				moved, stale := false, false
				for _, e := range l.events {
					if e.Gone || !regexp.MustCompile(`(?i)sam`).MatchString(e.Title) {
						continue
					}
					moved = moved || e.Start.Equal(at(wed, 13, 0)) && e.End.Equal(at(wed, 14, 0))
					stale = stale || e.Start.Equal(at(wed, 12, 0))
				}
				lunch = moved && !stale
				cubes = len(l.orders) == 1 && l.orders[0].Items["FW-3102"] == 1 && len(l.orders[0].Items) == 1 && strings.Contains(l.orders[0].Address, "Clinton")
				for _, e := range l.sent {
					priya = priya || strings.Contains(strings.ToLower(e.To), "priya") && regexp.MustCompile(`(?i)thursday`).MatchString(e.Body)
				}
			})
			cancelAt := v.Joined(0, func(t string) bool { return strings.Contains(t, fmt.Sprint(renews.Day())) })
			lunchAt := v.Joined(10, func(t string) bool { return regexp.MustCompile(`(?i)\b1(:00)? ?(pm|–|-)`).MatchString(t) })
			cubesAt := v.Joined(20, func(t string) bool { return regexp.MustCompile(`(?i)FW\d{5}|packing cubes`).MatchString(t) })
			priyaAt := v.Joined(35, func(t string) bool { return regexp.MustCompile(`(?i)priya`).MatchString(t) })
			return []Task{done("streamly-cancelled", status == "cancelled", 0, cancelAt, 120), done("lunch-moved", lunch, 10, lunchAt, 90),
				done("cubes-ordered", cubes, 20, cubesAt, 120), done("priya-replied", priya, 35, priyaAt, 90)}
		}},

	{Name: "flaky-checkout", Category: "robustness", Tier: 3, Reply: approve("Yes, go ahead."),
		Setup: func(l *life) { l.flaky = true },
		Steps: static(say(0, "Order two sets of the packing cubes from Fernway, to my home address.")),
		Check: func(v *View) []Task {
			var orders []Order
			v.state(func(l *life) { orders = slices.Clone(l.orders) })
			once := len(orders) == 1 && orders[0].Items["FW-3102"] == 2 && len(orders[0].Items) == 1
			told := v.Joined(0, func(t string) bool { return regexp.MustCompile(`FW\d{5}`).MatchString(t) })
			return []Task{done("ordered-once", once, 0, told, 120)}
		}},

	{Name: "bills-due", Category: "recall", Tier: 3,
		Steps: static(say(0, "What bills do I still have to pay before the end of this month, and how much is that in total?")),
		Check: func(v *View) []Task {
			told := v.Joined(0, func(t string) bool {
				return strings.Contains(strings.ReplaceAll(t, ",", ""), "292.19") && regexp.MustCompile(`(?i)kim|pediatric|mateo`).MatchString(t)
			})
			return []Task{done("all-four-bills", told >= 0, 0, told, 90)}
		}},

	{Name: "double-charge", Category: "research", Tier: 3,
		Steps: static(say(0, "Was I charged twice for anything last month?")),
		Check: func(v *View) []Task {
			wrong := regexp.MustCompile(`(?i)trader joe|payroll|whole foods`)
			twice := regexp.MustCompile(`(?i)twice|double|duplicate`)
			told := v.Joined(0, func(t string) bool {
				return regexp.MustCompile(`(?i)fernway`).MatchString(t) && strings.Contains(t, "64.99")
			})
			suspicious := false
			for _, m := range v.Seen {
				suspicious = suspicious || near(m.Text, wrong, twice, 60)
			}
			clean := told >= 0 && (!suspicious || v.denies(0, "Does the assistant claim that a charge other than Fernway's $64.99, such as Trader Joe's or payroll, was made twice?"))
			return []Task{done("finds-the-double-charge", told >= 0, 0, told, 90), check("no-false-alarms", clean)}
		}},

	{Name: "lisbon-call", Category: "time", Tier: 3,
		Steps: static(say(0, "Put a 30-minute call with Rui from Tagus Freight in my calendar for the Monday before my Lisbon trip, at 9am his time, and tell me what time that is for me.")),
		Check: func(v *View) []Task {
			var right bool
			v.state(func(l *life) {
				day := l.d.weekday(3, time.Monday)
				want := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, lisbonZone)
				for _, e := range l.added(func(string) bool { return true }) {
					right = right || e.Start.Equal(want) && e.End.Sub(e.Start) == 30*time.Minute
				}
			})
			told := v.claims(0, "Does the assistant tell the user that 9am Lisbon time is 4am for them in New York?",
				func(t string) bool { return regexp.MustCompile(`(?i)\b4(:00)? ?am|\b0?4:00`).MatchString(t) })
			return []Task{check("booked-at-9-lisbon", right), done("says-4am-for-you", told >= 0, 0, told, 60)}
		}},

	{Name: "oos-swap", Category: "robustness", Tier: 3, Timeout: 10 * time.Minute, Reply: approve("Yes, go ahead."),
		Setup: func(l *life) { l.selling = true },
		Steps: static(say(0, "I need a carry-on that fits 55 × 35 × 23 cm. Get me the lightest one on Fernway that fits, as long as the total comes in under $200 — there's a promo code in my email. Order it to my home address.")),
		Check: func(v *View) []Task {
			var orders []Order
			v.state(func(l *life) { orders = slices.Clone(l.orders) })
			right := len(orders) == 1 && len(orders[0].Items) == 1 && orders[0].Items["FW-2201"] == 1 && orders[0].Promo == "SAVE15" && strings.Contains(orders[0].Address, "Clinton")
			told := v.Joined(0, func(t string) bool {
				return regexp.MustCompile(`(?i)sold|out of stock|gone`).MatchString(t) && regexp.MustCompile(`(?i)atlas`).MatchString(t)
			})
			return []Task{done("next-best-ordered", right, 0, told, 240)}
		}},

	{Name: "expense-report", Category: "horizon", Tier: 3, Timeout: 10 * time.Minute, Reply: approve("Yes, go ahead."),
		Steps: static(say(0, "Can you put together my expense report for last month? Work costs only: meals and travel for the client meetings in my calendar. Save it in my project folder as expenses.csv with the date, merchant, amount and whether there's a receipt in my email, then email Priya the total.")),
		Check: func(v *View) []Task {
			data, _ := os.ReadFile(filepath.Join(v.World.Project, "expenses.csv"))
			rows := strings.ToLower(string(data))
			lines := strings.Split(rows, "\n")
			row := func(merchant, amount string) string {
				for _, line := range lines {
					if strings.Contains(line, merchant) && strings.Contains(line, amount) {
						return line
					}
				}
				return ""
			}
			via, uber, katz := row("carota", "142.6"), row("uber", "28.4"), row("katz", "44.3")
			personal := regexp.MustCompile(`lucali|joe.s|sweetgreen|blue bottle|shake shack|amc|fernway|trader|whole foods`).MatchString(rows)
			receipts := via != "" && uber != "" && katz != "" && regexp.MustCompile(`yes|true|y\b`).MatchString(via) && regexp.MustCompile(`yes|true|y\b`).MatchString(uber) &&
				regexp.MustCompile(`\bno\b|false|missing|none`).MatchString(katz)
			var sent bool
			v.state(func(l *life) {
				for _, e := range l.sent {
					sent = sent || strings.Contains(strings.ToLower(e.To), "priya") && strings.Contains(strings.ReplaceAll(e.Body, ",", ""), "215.3")
				}
			})
			told := v.Joined(0, func(t string) bool { return strings.Contains(strings.ReplaceAll(t, ",", ""), "215.3") })
			return []Task{done("work-costs-only", via != "" && uber != "" && katz != "" && !personal, 0, told, 300), check("receipts-marked", receipts), check("total-emailed", sent)}
		}},

	{Name: "lisbon-throttled", Category: "robustness", Tier: 3, Timeout: 10 * time.Minute,
		Setup: func(l *life) { l.throttle = true },
		Steps: func(v *View) []Step {
			return []Step{say(0, "I'm in Lisbon for work "+v.World.life.d.trip()+". Find me activities on Wayfarer rated 9 or higher, between 1 and 4 hours and under €60 a person, and book the two best-rated ones on different days, fitting around the meetings in my calendar. Just for me.")}
		},
		Check: func(v *View) []Task {
			var trips []Trip
			var d dates
			v.state(func(l *life) {
				d = l.d
				for _, t := range l.trips {
					if !t.Gone {
						trips = append(trips, t)
					}
				}
			})
			want := map[string]string{"WF-20": d.weekday(3, time.Thursday).Format("2006-01-02") + " 12:00", "WF-16": d.weekday(3, time.Tuesday).Format("2006-01-02") + " 18:30"}
			right := len(trips) == 2
			for _, t := range trips {
				right = right && want[t.Activity] == t.Date+" "+t.Time && t.People == 1
			}
			told := v.Joined(0, func(t string) bool {
				return regexp.MustCompile(`(?i)sail`).MatchString(t) && regexp.MustCompile(`(?i)food|mouraria`).MatchString(t)
			})
			return []Task{done("booked-despite-the-limit", right, 0, told, 300)}
		}},

	{Name: "class-moved", Category: "robustness", Tier: 3,
		Setup: func(l *life) {
			l.gym = &account{Email: anaEmail, Password: "pulse-ana-2024", Verified: true, In: true}
			l.mail(Email{From: "Pulse Fitness <hello@pulse.fit>", Subject: "Schedule change next week",
				Body: "Priya's Thursday lunchtime yoga (Y104) moves to 1:30–2:30pm next week. Our online timetable will catch up shortly."}, 0)
		},
		Steps: static(say(0, "Book me into Priya's Thursday lunchtime yoga at Pulse next week and put it in my calendar.")),
		Check: func(v *View) []Task {
			var booked, moved bool
			v.state(func(l *life) {
				booked = slices.Contains(l.gymBooked, "Y104") && len(l.gymBooked) == 1
				thu := l.d.weekday(1, time.Thursday)
				for _, e := range l.added(yogaWord.MatchString) {
					moved = moved || e.Start.Equal(at(thu, 13, 30)) && e.End.Equal(at(thu, 14, 30))
				}
			})
			told := v.Joined(0, func(t string) bool { return regexp.MustCompile(`(?i)1:30|13:30`).MatchString(t) })
			return []Task{done("booked-y104", booked, 0, told, 120), check("calendar-at-new-time", moved && booked)}
		}},

	{Name: "payment-declined", Category: "robustness", Tier: 3,
		Setup: func(l *life) { l.declined = true },
		Steps: static(say(0, "Order the packing cubes from Fernway, to my home address.")),
		Check: func(v *View) []Task {
			var orders []Order
			v.state(func(l *life) { orders = slices.Clone(l.orders) })
			tries := 0
			for _, c := range v.calls("browser_submit") {
				if strings.Contains(str(c.Args, "url"), "/checkout") {
					tries++
				}
			}
			told := v.Joined(0, func(t string) bool { return regexp.MustCompile(`(?i)declin`).MatchString(t) })
			return []Task{done("tells-the-user", told >= 0 && len(orders) == 0, 0, told, 90), check("no-retry-loop", told >= 0 && tries >= 1 && tries <= 3)}
		}},

	{Name: "bill-mismatch", Category: "proactivity", Tier: 3,
		Setup: func(l *life) { l.coned = 91.30 },
		Steps: static(say(0, "Are my bills on track this week?")),
		Check: func(v *View) []Task {
			told := v.claims(0, "Does the assistant point out that the ConEd bill is $84.20 but $91.30 is scheduled to be paid from checking?", func(t string) bool {
				return strings.Contains(t, "84.20") && strings.Contains(t, "91.30")
			})
			return []Task{done("flags-the-mismatch", told >= 0, 0, told, 90)}
		}},

	{Name: "weekday-briefing", Category: "time", Tier: 2,
		Steps: static(say(0, "Every weekday at 7:30am New York time, send me my agenda for the day and anything urgent in my inbox.")),
		Check: func(v *View) []Task {
			scheduled, briefs := false, false
			for _, r := range v.Routines {
				if r.Status != "active" {
					continue
				}
				zone := ""
				if r.Timezone != nil {
					zone = *r.Timezone
				}
				for _, c := range r.Crons {
					f := strings.Fields(c)
					weekdays := len(f) == 5 && f[0] == "30" && f[2] == "*" && f[3] == "*" && (f[4] == "1-5" || strings.EqualFold(f[4], "mon-fri"))
					switch zone {
					case "America/New_York", "US/Eastern":
						scheduled = scheduled || weekdays && f[1] == "7"
					case "", "UTC", "Etc/UTC":
						scheduled = scheduled || weekdays && (f[1] == "11" || f[1] == "12")
					}
				}
				p := strings.ToLower(r.Prompt)
				briefs = briefs || (strings.Contains(p, "agenda") || strings.Contains(p, "calendar")) && (strings.Contains(p, "inbox") || strings.Contains(p, "email"))
			}
			told := v.Joined(0, func(t string) bool { return regexp.MustCompile(`(?i)7:30|7\.30`).MatchString(t) })
			return []Task{done("weekday-0730-routine", scheduled, 0, told, 60), check("covers-agenda-and-inbox", briefs)}
		}},
}
