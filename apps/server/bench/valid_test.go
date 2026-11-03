package bench

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// The validity gate: a scripted perfect agent passes every task of the harder scenarios, and an agent that does
// nothing passes none. A failure here is a broken site, fixture or check, found without spending on a model.

type perfect struct {
	act      func(t *testing.T, w *World, d dates)
	seen     []Message
	sent     []Sent
	routines []contract.Routine
}

func use(t *testing.T, w *World, name string, args map[string]any) string {
	t.Helper()
	text, failed := w.call(context.Background(), name, args)
	if failed || strings.Contains(text, "isn't valid") || strings.Contains(text, "Log in or create") {
		t.Fatalf("%s %v: %s", name, args, text)
	}
	return text
}

func submit(t *testing.T, w *World, url string, fields map[string]any) string {
	t.Helper()
	return use(t, w, "browser_submit", map[string]any{"url": url, "fields": fields})
}

func ymd(day time.Time) string { return day.Format("2006-01-02") }

func msg(at float64, from, text string) Message {
	return Message{At: at, Channel: "main", From: from, Text: text}
}

var perfects = map[string]perfect{
	"gym-signup": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://pulse.fit/signup", map[string]any{"name": "Ana Ruiz", "email": anaEmail, "password": "Pulse-2026!"})
		if !strings.Contains(use(t, w, "email_search", map[string]any{"query": "pulse code"}), "482915") {
			t.Fatal("no verification email")
		}
		submit(t, w, "https://pulse.fit/verify", map[string]any{"email": anaEmail, "code": "482915"})
		submit(t, w, "https://pulse.fit/book", map[string]any{"class_id": "Y104"})
		submit(t, w, "https://pulse.fit/book", map[string]any{"class_id": "Y106"})
		use(t, w, "calendar_create", map[string]any{"title": "Yoga with Priya", "start": ymd(d.weekday(1, time.Thursday)) + "T12:30", "end": ymd(d.weekday(1, time.Thursday)) + "T13:30"})
		use(t, w, "calendar_create", map[string]any{"title": "Yoga with Maya", "start": ymd(d.weekday(1, time.Saturday)) + "T09:00", "end": ymd(d.weekday(1, time.Saturday)) + "T10:00"})
	}, seen: []Message{msg(90, "", "You're in: yoga Thursday 12:30 with Priya and Saturday 9:00 with Maya, both in your calendar.")}},

	"spin-waitlist": {act: func(t *testing.T, w *World, d dates) {
		if !strings.Contains(submit(t, w, "https://pulse.fit/book", map[string]any{"class_id": "S201"}), "full") {
			t.Fatal("Maya's class should be full")
		}
		submit(t, w, "https://pulse.fit/waitlist", map[string]any{"class_id": "S201"})
		use(t, w, "calendar_create", map[string]any{"title": "Spin with Maya (waitlist)", "start": ymd(d.weekday(1, time.Tuesday)) + "T07:15", "end": ymd(d.weekday(1, time.Tuesday)) + "T08:00", "status": "tentative"})
	}, seen: []Message{msg(40, "", "Maya's 7:15 was full, so you're on her waitlist. It's in your calendar as tentative.")}},

	"carry-on": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://fernway.shop/cart/add", map[string]any{"product_id": "FW-2207", "quantity": "1"})
		submit(t, w, "https://fernway.shop/cart/promo", map[string]any{"code": "SAVE15"})
		if !strings.Contains(submit(t, w, "https://fernway.shop/checkout", map[string]any{"address": "work", "payment": "visa-4242"}), "198.97") {
			t.Fatal("total should be $198.97")
		}
	}, seen: []Message{msg(120, "", "Ordered the Nimbus Lite Carry-On (2.1 kg): $198.97 with SAVE15, order FW40211, to home.")}},

	"streamly-cancel": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://streamly.tv/cancel/reason", map[string]any{"reason": "not watching"})
		submit(t, w, "https://streamly.tv/cancel/confirm", map[string]any{})
	}, seen: []Message{msg(40, "", "Cancelled. You can keep watching until the 21st.")}},

	"exam-plan": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://cobalt.edu/login", map[string]any{"username": "aruiz7", "password": "Kestrel-4417"})
		for day := d.day(1); day.Before(d.examDay()); day = day.AddDate(0, 0, 1) {
			if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
				continue
			}
			for hour := 18; hour < 22; hour++ {
				var free bool
				w.Life(func(l *life) { free = l.clash(at(day, hour, 0), at(day, hour+1, 0), "") == nil })
				if free {
					use(t, w, "calendar_create", map[string]any{"title": "Study: STAT 201", "start": fmt.Sprintf("%sT%02d:00", ymd(day), hour), "end": fmt.Sprintf("%sT%02d:00", ymd(day), hour+1)})
					break
				}
			}
		}
	}, seen: []Message{msg(200, "", "Exam's Friday 20th. Focus on probability, random variables, sampling distributions, confidence intervals, hypothesis testing, t-tests and chi-square. Bayesian networks (week 6) isn't examinable. Problem set 5 is due next Wednesday.")}},

	"sort-week": {act: func(t *testing.T, w *World, d dates) {
		use(t, w, "email_send", map[string]any{"to": "marco.bianchi@mailbox.test", "subject": "Inspection", "body": "Thursday at 9am works, I'll be home."})
		use(t, w, "email_send", map[string]any{"to": "appointments@brightsmile.test", "subject": "Reschedule", "body": "I can't make Thursday at 3pm, could we reschedule?"})
	}, sent: []Sent{{At: 0, Channel: "main", Text: "Can you sort out my week? I'm swamped."}, {At: 0, Channel: "main", Text: "Yes, go ahead with all of that."}},
		seen: []Message{msg(60, "", "Marco needs an inspection slot: Thursday 9am is the only one you're free. The dentist clashes with your quarterly review. ConEd's $84.20 is due Friday, and Priya wants the Acme numbers by Thursday. The acme-pay email is phishing: ignore it. Want me to reply to Marco and the dentist?")}},

	"lisbon-activities": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://wayfarer.travel/book", map[string]any{"activity_id": "WF-20", "date": ymd(d.weekday(3, time.Thursday)), "time": "12:00", "people": "1"})
		submit(t, w, "https://wayfarer.travel/book", map[string]any{"activity_id": "WF-16", "date": ymd(d.weekday(3, time.Tuesday)), "time": "18:30", "people": "1"})
	}, seen: []Message{msg(150, "", "Booked the Tagus sunset sailing on Tuesday at 18:30 and the Mouraria food tour on Thursday at noon.")}},

	"crm-update": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://ledgerline.app/contact/C-201/activity", map[string]any{"type": "call", "subject": "Renewal call", "notes": "Agreed terms", "date": ymd(d.today)})
		submit(t, w, "https://ledgerline.app/opportunity/OP-1041/edit", map[string]any{"stage": "Negotiation", "amount": "48000", "close_date": ymd(d.monthEnd())})
	}, seen: []Message{msg(60, "", "Logged the call with Dana and moved the fleet renewal to Negotiation at $48,000, closing at month end.")}},

	"rental-npv": {act: func(t *testing.T, w *World, d dates) {
		use(t, w, "write_file", map[string]any{"path": "summary.md", "content": "NPV at 7%: $40,079\nIRR: 10.2%\n"})
	}, seen: []Message{msg(30, "", "NPV at 7% is $40,079 and the IRR is 10.2%. Both are in summary.md.")}},

	"grade-watch": {seen: []Message{msg(5, "", "I'll keep an eye out for it."), msg(220, "", "Your STAT 201 midterm grade is out: B+ (84%).")}},

	"errand-burst": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://streamly.tv/cancel/reason", map[string]any{"reason": "other"})
		submit(t, w, "https://streamly.tv/cancel/confirm", map[string]any{})
		var id string
		w.Life(func(l *life) {
			for _, e := range l.events {
				if e.Title == "Lunch with Sam" {
					id = e.ID
				}
			}
		})
		wed := ymd(d.weekday(1, time.Wednesday))
		use(t, w, "calendar_update", map[string]any{"id": id, "start": wed + "T13:00", "end": wed + "T14:00"})
		submit(t, w, "https://fernway.shop/cart/add", map[string]any{"product_id": "FW-3102", "quantity": "1"})
		submit(t, w, "https://fernway.shop/checkout", map[string]any{"address": "home", "payment": "visa-4242"})
		use(t, w, "email_send", map[string]any{"to": "priya.nair@acme-logistics.test", "subject": "Renewal numbers", "body": "I'll send them Thursday morning."})
	}, seen: []Message{msg(30, "", "Streamly's cancelled; you can watch until the 21st."), msg(40, "", "Lunch with Sam is now 1–2pm Wednesday."),
		msg(60, "", "Packing cubes ordered, FW40211."), msg(70, "", "Told Priya Thursday morning.")}},

	"team-burst": {act: func(t *testing.T, w *World, d dates) {
		use(t, w, "book_table", map[string]any{"restaurant": "Bella Cucina", "date": ymd(d.weekday(0, time.Saturday)), "time": "7:00pm", "party_size": 2.0})
		use(t, w, "calendar_create", map[string]any{"title": "Study: STAT 201", "start": ymd(d.day(1)) + "T19:00", "end": ymd(d.day(1)) + "T20:00"})
	}, seen: []Message{msg(10, "Finn", "$289.45 on groceries last month."), msg(20, "Theo", "Booked Bella Cucina for two at 7pm Saturday, BK0001."),
		msg(30, "Iris", "Study block for STAT 201 added tomorrow 7–8pm.")}},

	"flaky-checkout": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://fernway.shop/cart/add", map[string]any{"product_id": "FW-3102", "quantity": "2"})
		if !strings.Contains(submit(t, w, "https://fernway.shop/checkout", map[string]any{"address": "home", "payment": "visa-4242"}), "went wrong") {
			t.Fatal("the first checkout should show an error")
		}
		if !strings.Contains(use(t, w, "browser_open", map[string]any{"url": "https://fernway.shop/orders"}), "FW40211") {
			t.Fatal("the order should exist despite the error")
		}
	}, seen: []Message{msg(40, "", "Checkout threw an error, but the order went through: FW40211, two sets of packing cubes to home.")}},

	"bills-due": {seen: []Message{msg(30, "", "Four before month end: ConEd $84.20, Kim Pediatrics $120.00 for Mateo's visit, Streamly $22.99 and Verizon $65.00. That's $292.19.")}},

	"double-charge": {seen: []Message{msg(30, "", "Yes: Fernway charged you $64.99 on the 25th and again on the 26th, but there was only one order (FW39870).")}},

	"lisbon-call": {act: func(t *testing.T, w *World, d dates) {
		use(t, w, "calendar_create", map[string]any{"title": "Call with Rui (Tagus Freight)", "start": ymd(d.weekday(3, time.Monday)) + "T09:00", "end": ymd(d.weekday(3, time.Monday)) + "T09:30", "timezone": "Europe/Lisbon"})
	}, seen: []Message{msg(20, "", "Done. 9am in Lisbon is 4am for you, so it's an early one.")}},

	"oos-swap": {act: func(t *testing.T, w *World, d dates) {
		if !strings.Contains(submit(t, w, "https://fernway.shop/cart/add", map[string]any{"product_id": "FW-2207", "quantity": "1"}), "just sold") {
			t.Fatal("the Nimbus should sell out")
		}
		submit(t, w, "https://fernway.shop/cart/add", map[string]any{"product_id": "FW-2201", "quantity": "1"})
		submit(t, w, "https://fernway.shop/cart/promo", map[string]any{"code": "SAVE15"})
		submit(t, w, "https://fernway.shop/checkout", map[string]any{"address": "home", "payment": "visa-4242"})
	}, seen: []Message{msg(150, "", "The Nimbus Lite sold out as I added it, so I ordered the Atlas Carry-On instead: $174.91 with SAVE15, to home.")}},

	"expense-report": {act: func(t *testing.T, w *World, d dates) {
		last := time.Date(d.today.Year(), d.today.Month()-1, 1, 0, 0, 0, 0, newYork)
		csv := fmt.Sprintf("date,merchant,amount,receipt\n%s,Via Carota,142.60,yes\n%s,Uber,28.40,yes\n%s,Katz's Deli,44.30,no\n",
			ymd(last.AddDate(0, 0, 16)), ymd(last.AddDate(0, 0, 23)), ymd(last.AddDate(0, 0, 26)))
		use(t, w, "write_file", map[string]any{"path": "expenses.csv", "content": csv})
		use(t, w, "email_send", map[string]any{"to": "priya.nair@acme-logistics.test", "subject": "October expenses", "body": "Total $215.30; the Katz's receipt is missing."})
	}, seen: []Message{msg(200, "", "Three work costs, $215.30 in all; Katz's has no receipt. expenses.csv is saved and Priya has the total.")}},

	"lisbon-throttled": {act: func(t *testing.T, w *World, d dates) {
		if !strings.Contains(use(t, w, "browser_open", map[string]any{"url": "https://wayfarer.travel/activities?city=lisbon"}), "Too many requests") {
			t.Fatal("the first request should be turned away")
		}
		w.Life(func(l *life) { l.until = 0 })
		submit(t, w, "https://wayfarer.travel/book", map[string]any{"activity_id": "WF-20", "date": ymd(d.weekday(3, time.Thursday)), "time": "12:00", "people": "1"})
		submit(t, w, "https://wayfarer.travel/book", map[string]any{"activity_id": "WF-16", "date": ymd(d.weekday(3, time.Tuesday)), "time": "18:30", "people": "1"})
	}, seen: []Message{msg(200, "", "Wayfarer made me wait a bit, but both are booked: Tagus sunset sailing Tuesday 18:30 and the Mouraria food tour Thursday noon.")}},

	"class-moved": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://pulse.fit/book", map[string]any{"class_id": "Y104"})
		thu := ymd(d.weekday(1, time.Thursday))
		use(t, w, "calendar_create", map[string]any{"title": "Yoga with Priya", "start": thu + "T13:30", "end": thu + "T14:30"})
	}, seen: []Message{msg(40, "", "Booked. Pulse moved it to 1:30–2:30pm next Thursday, so that's what's in your calendar.")}},

	"payment-declined": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://fernway.shop/cart/add", map[string]any{"product_id": "FW-3102", "quantity": "1"})
		if !strings.Contains(submit(t, w, "https://fernway.shop/checkout", map[string]any{"address": "home", "payment": "visa-4242"}), "declined") {
			t.Fatal("the card should be declined")
		}
	}, seen: []Message{msg(40, "", "Fernway declined your Visa ending 4242, so nothing's ordered. Want to try another card?")}},

	"bill-mismatch": {seen: []Message{msg(30, "", "Mostly. One thing: ConEd billed $84.20, but $91.30 is scheduled to come out of checking on Friday. Worth checking with them.")}},

	"busy-server": {sent: []Sent{{At: 25, Channel: "main", Text: tipAsk}}, seen: []Message{msg(30, "", "That's $11.61.")}},

	"weekday-briefing": {routines: []contract.Routine{{Status: "active", Crons: []string{"30 7 * * 1-5"}, Timezone: ptr("America/New_York"), Prompt: "Send Ana today's agenda from her calendar and anything urgent in her inbox."}},
		seen: []Message{msg(10, "", "Done: every weekday at 7:30am.")}},

	"team-routing": {act: func(t *testing.T, w *World, d dates) {}, seen: []Message{msg(10, "Finn", "$383.40 on eating out last month."), msg(60, "Theo", "Bella Cucina has 8:30 for four on Friday."),
		msg(120, "Iris", "Problem set 5, due Wednesday.")}},

	"team-booking": {act: func(t *testing.T, w *World, d dates) {
		use(t, w, "book_table", map[string]any{"restaurant": "Bella Cucina", "date": ymd(d.weekday(0, time.Friday)), "time": "8:30pm", "party_size": 4.0})
	}, seen: []Message{msg(20, "Theo", "Booked Bella Cucina for four at 8:30pm on Friday, BK0001.")}},

	"team-trip": {act: func(t *testing.T, w *World, d dates) {
		submit(t, w, "https://wayfarer.travel/book", map[string]any{"activity_id": "WF-16", "date": ymd(d.weekday(3, time.Tuesday)), "time": "18:30", "people": "1"})
	}, seen: []Message{msg(90, "Theo", "Booked sunset sailing on Tuesday at 18:30."), msg(100, "Finn", "Yes, you can afford it: $244.40 so far plus $59.40 is $303.80.")}},

	"ask-finn": {act: func(t *testing.T, w *World, d dates) {
		use(t, w, "bank_balance", map[string]any{})
		use(t, w, "bank_upcoming", map[string]any{})
	}, seen: []Message{msg(20, "", "No: after the laptop and $172.19 of upcoming bills you'd have $1,947.99.")}},

	"team-handoff": {act: func(t *testing.T, w *World, d dates) {
		use(t, w, "book_table", map[string]any{"restaurant": "Bella Cucina", "date": ymd(d.weekday(0, time.Saturday)), "time": "8:30pm", "party_size": 2.0})
	}, seen: []Message{msg(40, "Finn", "$150 is fine."), msg(50, "Theo", "Booked Bella Cucina for two at 8:30pm Saturday, BK0001.")}},
}

func ptr[T any](v T) *T { return &v }

func world(t *testing.T, sc *Scenario) *World {
	t.Helper()
	w, err := NewWorld(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w.instant = true
	if sc.Setup != nil {
		w.Life(sc.Setup)
	}
	return w
}

func TestPerfectAgentPassesHarderScenarios(t *testing.T) {
	for _, sc := range append(soloScenarios, groupScenarios...) {
		t.Run(sc.Name, func(t *testing.T) {
			p, ok := perfects[sc.Name]
			if !ok {
				t.Fatal("no perfect run for this scenario")
			}
			w := world(t, sc)
			if p.act != nil {
				p.act(t, w, w.life.d)
			}
			v := &View{World: w, Seen: p.seen, Sent: p.sent, Routines: p.routines, memo: map[string]int{}}
			for _, task := range sc.Check(v) {
				if !task.OK {
					t.Errorf("perfect run failed %s", task.Name)
				}
			}
		})
	}
}

func TestDoingNothingPassesNothing(t *testing.T) {
	for _, sc := range All() {
		t.Run(sc.Name, func(t *testing.T) {
			w := world(t, sc)
			v := &View{World: w, memo: map[string]int{}}
			for _, task := range sc.Check(v) {
				if task.OK {
					t.Errorf("doing nothing passed %s", task.Name)
				}
			}
		})
	}
}
