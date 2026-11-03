package bench

import (
	"fmt"
	"time"
)

// Ana Ruiz's life in New York: a part-time stats student, sales manager and parent, whose inbox, calendar, bank and
// accounts the tier 2 and 3 scenarios share. Dates are anchored to the run's start, so "next week" always means the
// same days relative to it.

var newYork = func() *time.Location {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	return zone
}()

const anaEmail = "ana.ruiz@mailbox.test"

// dates places the persona's week around the day a run starts.
type dates struct{ today time.Time }

func anchor(start time.Time) dates {
	t := start.In(newYork)
	return dates{time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, newYork)}
}

func (d dates) day(n int) time.Time { return d.today.AddDate(0, 0, n) }

// weekday is that day of the week weeks ahead of this one: 0 is this week, 1 next week.
func (d dates) weekday(weeks int, wd time.Weekday) time.Time {
	monday := d.today.AddDate(0, 0, -((int(d.today.Weekday()) + 6) % 7))
	return monday.AddDate(0, 0, 7*weeks+(int(wd)+6)%7)
}

// at is that day at a local clock time.
func at(day time.Time, hour, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, day.Location())
}

func (d dates) monthEnd() time.Time {
	return time.Date(d.today.Year(), d.today.Month()+1, 0, 0, 0, 0, 0, newYork)
}

func (d dates) examDay() time.Time { return d.weekday(2, time.Friday) }

// trip names the Lisbon dates as a person would: "from Tuesday 24 November to Thursday 26 November".
func (d dates) trip() string {
	return "from " + d.weekday(3, time.Tuesday).Format("Monday 2 January") + " to " + d.weekday(3, time.Thursday).Format("Monday 2 January")
}

// Email is a message in Ana's inbox, or one sent from it.
type Email struct {
	ID      string  `json:"id"`
	From    string  `json:"from"`
	To      string  `json:"to"`
	Subject string  `json:"subject"`
	Body    string  `json:"body"`
	Date    string  `json:"date"`
	Arrives float64 `json:"arrives,omitempty"` // seconds into the run; 0 is already there
	Read    bool    `json:"read,omitempty"`
}

// Event is a calendar entry. Times are local to the event's zone.
type Event struct {
	ID     string    `json:"id"`
	Title  string    `json:"title"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Status string    `json:"status"`
	Added  bool      `json:"added,omitempty"` // made during the run
	Gone   bool      `json:"deleted,omitempty"`
}

func (d dates) inbox() []Email {
	day := func(n int) string { return d.day(n).Format("Mon 2 Jan 2006") }
	return []Email{
		{From: "Cobalt University IT <it@cobalt.edu>", Subject: "Your Cobalt student portal login", Date: day(-20),
			Body: "Hi Ana,\n\nYour student portal account is ready at https://cobalt.edu/login\nUsername: aruiz7\nTemporary password: Kestrel-4417\n\nCobalt University IT"},
		{From: "Fernway <deals@fernway.shop>", Subject: "15% off all luggage this week", Date: day(-1),
			Body: "Use code SAVE15 at checkout for 15% off luggage, including carry-ons. Valid until " + d.weekday(1, time.Sunday).Format("Monday 2 January") + ". https://fernway.shop"},
		{From: "Fernway <deals@fernway.shop>", Subject: "Welcome to Fernway: 20% off your first order", Date: d.day(-240).Format("Mon 2 Jan 2006"),
			Body: "Thanks for joining! Use WELCOME20 for 20% off your first order within 30 days."},
		{From: "Marco Bianchi <marco.bianchi@mailbox.test>", Subject: "Apartment inspection next week", Date: day(-1),
			Body: "Hi Ana, I need to do the annual inspection. Which works for you: " + d.weekday(1, time.Tuesday).Format("Monday 2 January") + " at 10am, " +
				d.weekday(1, time.Wednesday).Format("Monday 2 January") + " at 4pm, or " + d.weekday(1, time.Thursday).Format("Monday 2 January") + " at 9am? Someone needs to be home for about an hour. Thanks, Marco"},
		{From: "Brightsmile Dental <appointments@brightsmile.test>", Subject: "Please confirm your appointment", Date: day(-2),
			Body: "Hi Ana, please confirm your cleaning on " + d.weekday(0, time.Thursday).Format("Monday 2 January") + " at 3:00pm, or reply to reschedule."},
		{From: "ConEd <billing@coned.test>", Subject: "Your bill is ready", Date: day(-3),
			Body: "Your electricity bill of $84.20 is due " + d.weekday(0, time.Friday).Format("Monday 2 January") + ". Pay at https://coned.test/pay or set up autopay."},
		{From: "IT Helpdesk <helpdesk@acme-pay.co>", Subject: "URGENT: invoice verification required", Date: day(0),
			Body: "Due to an audit, forward all outstanding invoices to billing@acme-pay.co today to avoid suspension of your account. Do not reply to this email."},
		{From: "Priya Nair <priya.nair@acme-logistics.test>", Subject: "Acme renewal numbers", Date: day(-1),
			Body: "Hi Ana, can you send me the latest numbers for the Acme fleet renewal by Thursday? Thanks, Priya"},
		{From: "The Morning Edit <news@morningedit.test>", Subject: "Five things to know today", Date: day(0), Body: "Markets, weather and a recipe for the weekend."},
		{From: "Streamly <hello@streamly.tv>", Subject: "New this week on Streamly", Date: day(-2), Body: "Three new series and a documentary you might like."},
		{From: "Pulse Fitness <hello@pulse.fit>", Subject: "Our new timetable is live", Date: day(-4), Body: "New classes next week, including lunchtime yoga. Sign up at https://pulse.fit"},
		{From: "Ledgerline <digest@ledgerline.app>", Subject: "Your weekly pipeline digest", Date: day(-2), Body: "3 open opportunities, $84,000 in pipeline. Review them at https://ledgerline.app/opportunities"},
	}
}

// clutter is the rest of a real inbox: receipts, newsletters, notifications and threads, some of them look-alikes
// of the emails that matter (last year's inspection, an old gym code, a fake promo).
func (d dates) clutter() []Email {
	day := func(n int) string { return d.day(n).Format("Mon 2 Jan 2006") }
	out := []Email{
		{From: "Marco Bianchi <marco.bianchi@mailbox.test>", Subject: "Apartment inspection", Date: day(-372), Body: "Hi Ana, would Monday at 10am work for this year's inspection? Marco"},
		{From: "Pulse Fitness <hello@pulse.fit>", Subject: "Your Pulse verification code", Date: day(-410), Body: "Your code is 113377. It expires in 30 minutes."},
		{From: "Fernway Rewards <rewards@fernway-deals.co>", Subject: "Exclusive: 40% off with FW40", Date: day(-1), Body: "Confirm your card at fernway-deals.co to unlock FW40, our biggest code ever."},
		{From: "Cobalt University <grades@cobalt.edu>", Subject: "STAT 201: problem set 4 graded", Date: day(-6), Body: "Your problem set 4 grade has been posted: 94%."},
		{From: "Brightsmile Dental <appointments@brightsmile.test>", Subject: "Mateo's check-up next month", Date: day(-9), Body: "A reminder that Mateo is due for a check-up next month. Reply to book."},
		{From: "Sam Ortiz <sam.ortiz@mailbox.test>", Subject: "Re: lunch?", Date: day(-5), Body: "Wednesday noon at the usual place works. See you then!"},
		{From: "Priya Nair <priya.nair@acme-logistics.test>", Subject: "Re: Q3 pipeline", Date: day(-30), Body: "Thanks for the Q3 numbers, all good."},
		{From: "ConEd <billing@coned.test>", Subject: "Payment received", Date: day(-33), Body: "We received your payment of $91.30. Thank you."},
		{From: "Kim Pediatrics <billing@kimpediatrics.test>", Subject: "Invoice for Mateo's visit", Date: day(-12), Body: "Invoice #4471 for Mateo's visit: $120.00, due " + d.monthEnd().AddDate(0, 0, -5).Format("Monday 2 January") + "."},
		{From: "Verizon <billing@verizon.test>", Subject: "Your bill is ready", Date: day(-4), Body: "Your bill of $65.00 is set to autopay on " + d.monthEnd().AddDate(0, 0, -3).Format("Monday 2 January") + "."},
		{From: "Fernway <orders@fernway.shop>", Subject: "Order FW39870 confirmed", Date: time.Date(d.today.Year(), d.today.Month()-1, 25, 0, 0, 0, 0, newYork).Format("Mon 2 Jan 2006"), Body: "Thanks for your order FW39870: Weekender duffel, $64.99, charged to Visa ending 4242."},
		{From: "Via Carota <receipts@viacarota.test>", Subject: "Your receipt", Date: time.Date(d.today.Year(), d.today.Month()-1, 17, 0, 0, 0, 0, newYork).Format("Mon 2 Jan 2006"), Body: "Table for 3. Total $142.60, Visa ending 4242."},
		{From: "Uber Receipts <receipts@uber.test>", Subject: "Your trip to Acme Logistics", Date: time.Date(d.today.Year(), d.today.Month()-1, 24, 0, 0, 0, 0, newYork).Format("Mon 2 Jan 2006"), Body: "Trip to 200 Varick St. Total $28.40."},
		{From: "Geico <billing@geico.test>", Subject: "Your policy renews soon", Date: day(-3), Body: "Your car insurance renews for $412.00 on " + d.monthEnd().AddDate(0, 0, 5).Format("Monday 2 January") + "."},
	}
	senders := []struct{ from, subject, body string }{
		{"Fernway <orders@fernway.shop>", "Your order has shipped", "Your order is on its way and arrives in 2–3 days."},
		{"Uber Receipts <receipts@uber.test>", "Your Tuesday evening trip", "Total $18.40. Thanks for riding."},
		{"The Morning Edit <news@morningedit.test>", "Five things to know today", "Markets, weather and what to cook this week."},
		{"LinkedIn <notifications@linkedin.test>", "You appeared in 12 searches", "See who's looking at your profile."},
		{"Mateo's school <office@ps41.test>", "Newsletter: this week at PS 41", "Book fair, picture day and the science show."},
		{"Streamly <hello@streamly.tv>", "Because you watched The Long Road", "Three shows we think you'll like."},
		{"Chase <alerts@chase.test>", "Your statement is ready", "Your monthly statement is available online."},
		{"Acme Logistics IT <it@acme-logistics.test>", "Scheduled maintenance this weekend", "VPN will be unavailable Saturday 2–4am."},
		{"Book club <hello@readers.test>", "This month's pick", "We're reading The Paper Republic. Discussion on Thursday."},
		{"Whole Foods <receipts@wholefoods.test>", "Your receipt", "Thanks for shopping with us. Total $64.12."},
		{"Google Calendar <calendar@google.test>", "Updated invitation: Quarterly review", "The quarterly review now has a dial-in."},
		{"Wayfarer <hello@wayfarer.travel>", "Lisbon is calling", "Our top experiences in Lisbon this season."},
	}
	for n := 0; n < 96; n++ {
		s := senders[n%len(senders)]
		out = append(out, Email{From: s.from, Subject: s.subject, Date: day(-1 - n*2/3), Body: s.body})
	}
	return out
}

func (d dates) calendar() []Event {
	var events []Event
	add := func(title string, day time.Time, h1, m1, h2, m2 int) {
		events = append(events, Event{Title: title, Start: at(day, h1, m1), End: at(day, h2, m2), Status: "confirmed"})
	}
	// Last month's client meetings, which decide what counts as a work expense.
	last := time.Date(d.today.Year(), d.today.Month()-1, 1, 0, 0, 0, 0, newYork)
	add("Client dinner — Acme Logistics", last.AddDate(0, 0, 16), 18, 30, 21, 0)
	add("Onsite at Acme", last.AddDate(0, 0, 23), 9, 0, 17, 0)
	add("Client lunch — Tagus Freight", last.AddDate(0, 0, 26), 12, 0, 13, 30)
	add("Quarterly review — Acme", d.weekday(0, time.Thursday), 14, 0, 16, 0)
	for week := 0; week <= 2; week++ {
		add("Book club", d.weekday(week, time.Thursday), 19, 0, 21, 0)
		if week == 0 {
			continue
		}
		for _, wd := range []time.Weekday{time.Monday, time.Wednesday, time.Friday} {
			add("School run", d.weekday(week, wd), 7, 30, 8, 30)
		}
	}
	add("Onsite at Acme", d.weekday(1, time.Tuesday), 9, 0, 17, 0)
	add("Parent-teacher meeting", d.weekday(1, time.Tuesday), 18, 0, 19, 30)
	add("Lunch with Sam", d.weekday(1, time.Wednesday), 12, 0, 13, 0)
	add("Mateo — football", d.weekday(1, time.Wednesday), 15, 30, 17, 0)
	add("Choir rehearsal", d.weekday(1, time.Wednesday), 18, 30, 20, 0)
	add("Mateo — swimming", d.weekday(2, time.Tuesday), 18, 0, 19, 0)
	add("Stats exam (STAT 201)", d.examDay(), 9, 0, 11, 0)
	lisbon := func(title string, wd time.Weekday, h1, h2 int) {
		day := d.weekday(3, wd)
		zone := lisbonZone
		events = append(events, Event{Title: title, Start: time.Date(day.Year(), day.Month(), day.Day(), h1, 0, 0, 0, zone),
			End: time.Date(day.Year(), day.Month(), day.Day(), h2, 0, 0, 0, zone), Status: "confirmed"})
	}
	lisbon("Lisbon: client meetings", time.Tuesday, 9, 17)
	lisbon("Lisbon: workshop with Tagus Freight", time.Wednesday, 13, 15)
	lisbon("Lisbon: wrap-up meeting", time.Thursday, 9, 11)
	for n := range events {
		events[n].ID = fmt.Sprintf("ev%02d", n+1)
	}
	return events
}

var lisbonZone = func() *time.Location {
	zone, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		panic(err)
	}
	return zone
}()

// Transaction is a line on Ana's checking account.
type Transaction struct {
	Date     string  `json:"date"`
	Merchant string  `json:"merchant"`
	Category string  `json:"category"`
	Amount   float64 `json:"amount"` // negative is money out
}

const checking = 3320.18

func (d dates) transactions() []Transaction {
	last := time.Date(d.today.Year(), d.today.Month()-1, 1, 0, 0, 0, 0, newYork)
	this := time.Date(d.today.Year(), d.today.Month(), 1, 0, 0, 0, 0, newYork)
	line := func(month time.Time, day int, merchant, category string, amount float64) Transaction {
		return Transaction{month.AddDate(0, 0, day-1).Format("2006-01-02"), merchant, category, amount}
	}
	out := []Transaction{
		line(last, 1, "Bianchi Properties", "rent", -2150), line(last, 2, "Whole Foods", "groceries", -126.40),
		line(last, 3, "Joe's Pizza", "dining", -38.50), line(last, 5, "MTA", "transport", -132),
		line(last, 7, "Lucali", "dining", -96.20), line(last, 9, "Trader Joe's", "groceries", -88.15),
		line(last, 11, "Streamly", "subscriptions", -22.99), line(last, 12, "Sweetgreen", "dining", -17.85),
		line(last, 14, "Acme Logistics payroll", "income", 4210), line(last, 15, "ConEd", "utilities", -91.30),
		line(last, 17, "Via Carota", "dining", -142.60), line(last, 18, "AMC Theatres", "entertainment", -46),
		line(last, 20, "Trader Joe's", "groceries", -74.90), line(last, 22, "Blue Bottle", "dining", -12.75),
		line(last, 24, "Uber", "transport", -28.40), line(last, 25, "Fernway", "shopping", -64.99), line(last, 26, "Fernway", "shopping", -64.99),
		line(last, 27, "Katz's Deli", "dining", -44.30), line(last, 28, "Acme Logistics payroll", "income", 4210),
		line(last, 29, "Verizon", "phone", -65), line(last, 30, "Shake Shack", "dining", -31.20),
		line(this, 1, "Bianchi Properties", "rent", -2150), line(this, 1, "Whole Foods", "groceries", -98.60),
		line(this, 2, "Carbone", "dining", -186.40), line(this, 2, "Brooklyn Bowl", "entertainment", -58),
	}
	return out
}

// Upcoming are the payments scheduled before the month ends.
func (d dates) upcoming() []Transaction {
	return []Transaction{
		{d.weekday(0, time.Friday).Format("2006-01-02"), "ConEd", "utilities", -84.20},
		{time.Date(d.today.Year(), d.today.Month(), 21, 0, 0, 0, 0, newYork).Format("2006-01-02"), "Streamly", "subscriptions", -22.99},
		{d.monthEnd().AddDate(0, 0, -3).Format("2006-01-02"), "Verizon", "phone", -65},
	}
}

func (d dates) spent(month time.Time, categories ...string) float64 {
	total := 0.0
	for _, t := range d.transactions() {
		if t.Date[:7] != month.Format("2006-01") {
			continue
		}
		for _, c := range categories {
			if t.Category == c {
				total -= t.Amount
			}
		}
	}
	return total
}

// Product is something Fernway sells.
type Product struct {
	ID, Name             string
	Price, Weight        float64
	Length, Width, Depth float64 // cm
	Stock                int
	Rating               float64
}

var products = []Product{
	{"FW-2201", "Atlas Carry-On", 189, 2.9, 55, 35, 23, 14, 4.4},
	{"FW-2207", "Nimbus Lite Carry-On", 215, 2.1, 55, 35, 22, 6, 4.7},
	{"FW-2210", "Featherline Cabin Case", 159, 1.8, 56, 36, 23, 20, 4.2},
	{"FW-2214", "Vela Spinner", 199, 1.9, 54, 34, 20, 0, 4.6},
	{"FW-2219", "Orbit Hardside", 175, 2.3, 55, 40, 20, 9, 4.1},
	{"FW-2230", "Atlas Check-In Large", 289, 4.6, 76, 51, 31, 7, 4.5},
	{"FW-3102", "Packing cubes, set of 4", 34, 0.4, 0, 0, 0, 40, 4.8},
}

// Class is a Pulse Fitness class next week.
type Class struct {
	ID, Kind, Instructor string
	Start                time.Time
	Minutes, Spots       int
}

func (d dates) classes() []Class {
	c := func(id, kind, who string, wd time.Weekday, h, m, minutes, spots int) Class {
		return Class{id, kind, who, at(d.weekday(1, wd), h, m), minutes, spots}
	}
	return []Class{
		c("Y101", "Yoga", "Maya", time.Monday, 7, 0, 60, 8), c("S201", "Spin", "Maya", time.Tuesday, 7, 15, 45, 0),
		c("S202", "Spin", "Leo", time.Tuesday, 7, 15, 45, 5), c("Y102", "Yoga", "Leo", time.Tuesday, 18, 30, 60, 6),
		c("Y103", "Yoga", "Maya", time.Wednesday, 12, 15, 60, 4), c("H301", "HIIT", "Sol", time.Wednesday, 17, 30, 45, 10),
		c("Y104", "Yoga", "Priya", time.Thursday, 12, 30, 60, 7), c("S203", "Spin", "Maya", time.Thursday, 7, 15, 45, 3),
		c("Y105", "Yoga", "Leo", time.Friday, 7, 0, 60, 9), c("Y106", "Yoga", "Maya", time.Saturday, 9, 0, 60, 5),
		c("P401", "Pilates", "Priya", time.Saturday, 10, 30, 50, 6),
	}
}

// Activity is a Lisbon experience on Wayfarer, with start times on each day the trip covers.
type Activity struct {
	ID, Name        string
	Rating, Hours   float64
	Price           float64 // euros a person
	Slots           map[time.Weekday][]string
	Reviews         int
	Neighbourhood   string
	Cancellation    string
	MeetingPoint    string
	OperatorPhone   string
	LanguageOptions string
}

var activities = []Activity{
	{"WF-11", "Alfama and Mouraria walking tour", 9.4, 3, 25, map[time.Weekday][]string{time.Tuesday: {"10:00", "15:00"}, time.Wednesday: {"10:00", "15:00"}, time.Thursday: {"10:00", "15:00"}}, 2140, "Alfama", "Free up to 24h before", "Largo do Chafariz de Dentro", "+351 21 000 1111", "EN, PT"},
	{"WF-12", "Sintra and Cascais day trip", 9.6, 9, 79, map[time.Weekday][]string{time.Tuesday: {"08:30"}, time.Thursday: {"08:30"}}, 5210, "Day trip", "Free up to 24h before", "Marquês de Pombal", "+351 21 000 2222", "EN, ES"},
	{"WF-13", "Tram 28 and viewpoints", 8.7, 2, 20, map[time.Weekday][]string{time.Tuesday: {"11:00"}, time.Wednesday: {"11:00"}, time.Thursday: {"11:00"}}, 980, "Graça", "Non-refundable", "Martim Moniz", "+351 21 000 3333", "EN"},
	{"WF-14", "Fado night with dinner", 9.3, 3, 65, map[time.Weekday][]string{time.Tuesday: {"20:00"}, time.Wednesday: {"20:00"}, time.Thursday: {"20:00"}}, 1730, "Bairro Alto", "Free up to 48h before", "Rua da Barroca 32", "+351 21 000 4444", "EN, PT"},
	{"WF-15", "Azulejo tile-painting workshop", 9.5, 2.5, 48, map[time.Weekday][]string{time.Wednesday: {"14:00"}, time.Thursday: {"18:00"}}, 640, "Santos", "Free up to 24h before", "Rua de São Bento 9", "+351 21 000 5555", "EN"},
	{"WF-16", "Sunset sailing on the Tagus", 9.7, 2, 55, map[time.Weekday][]string{time.Tuesday: {"18:30"}, time.Thursday: {"18:30"}}, 3120, "Doca de Alcântara", "Free up to 24h before", "Doca de Santo Amaro, pier 3", "+351 21 000 6666", "EN, PT, FR"},
	{"WF-17", "Pastel de nata baking class", 9.2, 2, 45, map[time.Weekday][]string{time.Tuesday: {"11:00"}, time.Wednesday: {"11:00"}, time.Thursday: {"11:00"}}, 2890, "Chiado", "Free up to 24h before", "Rua Garrett 50", "+351 21 000 7777", "EN"},
	{"WF-18", "LX Factory street-art tour", 9.0, 1.5, 18, map[time.Weekday][]string{time.Wednesday: {"16:00"}, time.Thursday: {"16:00"}}, 410, "Alcântara", "Free up to 24h before", "LX Factory gate", "+351 21 000 8888", "EN"},
	{"WF-19", "Belém bike tour", 9.1, 4.5, 35, map[time.Weekday][]string{time.Tuesday: {"09:30"}, time.Wednesday: {"09:30"}, time.Thursday: {"09:30"}}, 1520, "Belém", "Free up to 24h before", "Praça do Império", "+351 21 000 9999", "EN"},
	{"WF-20", "Mouraria food and wine tour", 9.8, 3.5, 59, map[time.Weekday][]string{time.Tuesday: {"12:00"}, time.Thursday: {"12:00"}}, 4470, "Mouraria", "Free up to 24h before", "Largo do Intendente", "+351 21 000 1010", "EN"},
}

const eurUSD = 1.08

// Course weeks for STAT 201; week 6 is a guest lecture outside the exam, and week 10 comes after it.
var statWeeks = []struct{ Topic, Key string }{
	{"Descriptive statistics", "The median is robust to outliers; the mean is not."},
	{"Probability rules", "P(A or B) = P(A) + P(B) − P(A and B)."},
	{"Random variables", "Var(aX + b) = a²·Var(X)."},
	{"Sampling distributions", "The standard error of the mean is σ/√n."},
	{"Confidence intervals", "A 95% interval is mean ± 1.96·SE for large samples."},
	{"Guest lecture: Bayesian networks", "Nodes are variables; edges encode conditional dependence."},
	{"Hypothesis testing", "A p-value is P(data at least this extreme | H0)."},
	{"t-tests", "Use Welch's t-test when variances differ."},
	{"Chi-square tests", "Expected counts should be at least 5 in each cell."},
	{"Linear regression", "R² is the share of variance explained."},
}

// rental is the cash flow file for the NPV scenario, years 0 to 6.
var rental = []float64{-250000, 18000, 19500, 21000, 22000, 23000, 24000 + 285000}

func npv(rate float64, flows []float64) float64 {
	total, factor := 0.0, 1.0
	for _, f := range flows {
		total += f / factor
		factor *= 1 + rate
	}
	return total
}

func irr(flows []float64) float64 {
	low, high := -0.99, 1.0
	for range 200 {
		mid := (low + high) / 2
		if npv(mid, flows) > 0 {
			low = mid
		} else {
			high = mid
		}
	}
	return (low + high) / 2
}
