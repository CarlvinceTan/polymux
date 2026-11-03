package bench

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// life is the persona's state during one run: what the scenarios' checks read.
type life struct {
	now    func() float64 // seconds into the run, so timed emails arrive on time
	d      dates
	inbox  []Email
	sent   []Email
	events []Event

	gym       *account
	gymBooked []string
	gymWaits  []string
	classes   []Class

	cart     map[string]int
	promo    string
	orders   []Order
	flaky    bool            // the next checkout places the order but shows an error
	selling  bool            // the Nimbus sells out the moment it's added to a cart
	gone     map[string]bool // products that have sold out during the run
	declined bool            // Fernway's checkout declines the saved card
	coned    float64         // the ConEd payment scheduled at the bank, when it differs from the bill
	throttle bool            // Wayfarer turns requests away for a while, starting with the first
	until    float64         // when it lets requests through again

	streamly string // active, discounted, paused or cancelled

	cobalt bool // logged in to the student portal
	graded float64

	trips []Trip

	deals    map[string]*Deal
	activity []CRMActivity
}

type account struct {
	Email, Password, Code string
	Verified, In          bool
}

// Order is a placed Fernway order.
type Order struct {
	ID      string         `json:"id"`
	Items   map[string]int `json:"items"`
	Promo   string         `json:"promo,omitempty"`
	Total   float64        `json:"total"`
	Address string         `json:"address"`
}

// Trip is a booked Wayfarer activity.
type Trip struct {
	ID       string  `json:"id"`
	Activity string  `json:"activity"`
	Date     string  `json:"date"`
	Time     string  `json:"time"`
	People   int     `json:"people"`
	Total    float64 `json:"total"`
	Gone     bool    `json:"cancelled,omitempty"`
}

// Deal is a Ledgerline opportunity.
type Deal struct {
	ID, Name, Account, Stage, Close string
	Amount                          float64
}

// CRMActivity is a logged call, email or meeting.
type CRMActivity struct {
	Contact, Kind, Subject, Notes, Date string
}

func newLife(start time.Time, now func() float64) *life {
	d := anchor(start)
	l := &life{now: now, d: d, inbox: append(d.clutter(), d.inbox()...), events: d.calendar(), classes: d.classes(), cart: map[string]int{}, gone: map[string]bool{}, streamly: "active"}
	for n := range l.inbox {
		l.inbox[n].ID = fmt.Sprintf("m%02d", n+1)
		l.inbox[n].To = anaEmail
	}
	nextMonth := d.monthEnd().AddDate(0, 0, 15).Format("2006-01-02")
	l.deals = map[string]*Deal{
		"OP-1041": {"OP-1041", "Fleet renewal", "Acme Logistics", "Proposal", nextMonth, 42000},
		"OP-1052": {"OP-1052", "Warehouse pilot", "Acme Logistics", "Qualification", nextMonth, 12000},
		"OP-1060": {"OP-1060", "POS rollout", "Acme Foods", "Proposal", nextMonth, 30000},
	}
	return l
}

// mail delivers an email during the run.
func (l *life) mail(e Email, now float64) {
	e.ID, e.To, e.Arrives = fmt.Sprintf("m%02d", len(l.inbox)+1), anaEmail, now
	if e.Date == "" {
		e.Date = l.d.today.Format("Mon 2 Jan 2006")
	}
	l.inbox = append(l.inbox, e)
}

func officeTools() map[string]tool {
	text := map[string]any{"type": "string"}
	flag := map[string]any{"type": "boolean"}
	when := map[string]any{"type": "string", "description": "Local date and time, such as 2026-11-12T18:30"}
	zone := map[string]any{"type": "string", "description": "IANA timezone of start and end; defaults to America/New_York"}
	return map[string]tool{
		"email_search": {"Search the user's inbox, newest first, 25 results a page. An empty query lists everything.", map[string]any{"query": text, "unread_only": flag, "page": map[string]any{"type": "integer"}}, nil, 800 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) {
				found := w.life.search(str(a, "query"), a["unread_only"] == true)
				page := max(num(a, "page"), 1)
				from, to := min(len(found), (page-1)*25), min(len(found), page*25)
				return map[string]any{"results": found[from:to], "page": page, "total": len(found)}, nil
			}},
		"email_read": {"Read one email by id.", map[string]any{"id": text}, []string{"id"}, 500 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) { return w.life.read(str(a, "id")) }},
		"email_send": {"Send an email from the user's address. This really sends it.", map[string]any{"to": text, "subject": text, "body": text}, []string{"to", "subject", "body"}, time.Second,
			func(w *World, a map[string]any) (any, error) {
				e := Email{ID: fmt.Sprintf("s%02d", len(w.life.sent)+1), From: anaEmail, To: str(a, "to"), Subject: str(a, "subject"), Body: str(a, "body"), Arrives: w.now()}
				w.life.sent = append(w.life.sent, e)
				return map[string]any{"sent": true, "id": e.ID}, nil
			}},
		"calendar_list": {"List calendar events between two dates, inclusive. Times show their zone.", map[string]any{"start_date": text, "end_date": text}, []string{"start_date", "end_date"}, 600 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) {
				return w.life.list(str(a, "start_date"), str(a, "end_date"))
			}},
		"calendar_create": {"Add a calendar event.", map[string]any{"title": text, "start": when, "end": when, "timezone": zone, "status": map[string]any{"type": "string", "enum": []string{"confirmed", "tentative"}}},
			[]string{"title", "start", "end"}, 600 * time.Millisecond, func(w *World, a map[string]any) (any, error) { return w.life.create(a) }},
		"calendar_update": {"Change a calendar event; omitted fields stay as they are.", map[string]any{"id": text, "title": text, "start": when, "end": when, "timezone": zone, "status": text},
			[]string{"id"}, 600 * time.Millisecond, func(w *World, a map[string]any) (any, error) { return w.life.update(a) }},
		"calendar_delete": {"Delete a calendar event.", map[string]any{"id": text}, []string{"id"}, 600 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) { return w.life.remove(str(a, "id")) }},
		"bank_balance": {"The user's checking account balance.", map[string]any{}, nil, 700 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) {
				return map[string]any{"account": "Everyday Checking ••7731", "balance": checking, "currency": "USD"}, nil
			}},
		"bank_transactions": {"Checking account transactions for a month (YYYY-MM).", map[string]any{"month": text}, []string{"month"}, time.Second,
			func(w *World, a map[string]any) (any, error) {
				var out []Transaction
				for _, t := range w.life.d.transactions() {
					if strings.HasPrefix(t.Date, str(a, "month")) {
						out = append(out, t)
					}
				}
				return map[string]any{"month": str(a, "month"), "transactions": out}, nil
			}},
		"bank_upcoming": {"Payments scheduled from checking before the month ends.", map[string]any{}, nil, 700 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) {
				scheduled := w.life.d.upcoming()
				if w.life.coned != 0 {
					scheduled[0].Amount = -w.life.coned
				}
				return map[string]any{"scheduled": scheduled}, nil
			}},
	}
}

func (l *life) visible(now float64) []Email {
	var out []Email
	for _, e := range l.inbox {
		if e.Arrives <= now {
			out = append(out, e)
		}
	}
	return out
}

func (l *life) search(query string, unread bool) []map[string]any {
	words := strings.Fields(strings.ToLower(query))
	var out []map[string]any
	for _, e := range slices.Backward(l.visibleNow()) {
		hay := strings.ToLower(e.From + " " + e.Subject + " " + e.Body)
		match := true
		for _, word := range words {
			match = match && strings.Contains(hay, word)
		}
		if !match || unread && e.Read {
			continue
		}
		snippet := e.Body
		if len(snippet) > 120 {
			snippet = snippet[:120] + "…"
		}
		out = append(out, map[string]any{"id": e.ID, "from": e.From, "subject": e.Subject, "date": e.Date, "unread": !e.Read, "snippet": snippet})
	}
	return out
}

func (l *life) visibleNow() []Email { return l.visible(l.now()) }

func (l *life) read(id string) (any, error) {
	for n, e := range l.inbox {
		if e.ID == id && e.Arrives <= l.now() {
			l.inbox[n].Read = true
			return e, nil
		}
	}
	return nil, errors.New("no email with that id")
}

func parseWhen(value, zone string) (time.Time, error) {
	location := newYork
	if zone != "" {
		loaded, err := time.LoadLocation(zone)
		if err != nil {
			return time.Time{}, fmt.Errorf("unknown timezone %q", zone)
		}
		location = loaded
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.In(location), nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, value, location); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("use a time like 2026-11-12T18:30, not %q", value)
}

func describe(e Event) map[string]any {
	return map[string]any{"id": e.ID, "title": e.Title, "start": e.Start.Format("2006-01-02T15:04"), "end": e.End.Format("2006-01-02T15:04"),
		"timezone": e.Start.Location().String(), "status": e.Status}
}

func (l *life) list(from, to string) (any, error) {
	start, err := time.ParseInLocation("2006-01-02", from, newYork)
	if err != nil {
		return nil, errors.New("start_date must look like 2026-11-09")
	}
	end, err := time.ParseInLocation("2006-01-02", to, newYork)
	if err != nil {
		return nil, errors.New("end_date must look like 2026-11-15")
	}
	end = end.AddDate(0, 0, 1)
	var out []map[string]any
	events := slices.Clone(l.events)
	sort.SliceStable(events, func(i, j int) bool { return events[i].Start.Before(events[j].Start) })
	for _, e := range events {
		if !e.Gone && e.End.After(start) && e.Start.Before(end) {
			out = append(out, describe(e))
		}
	}
	return map[string]any{"events": out}, nil
}

func (l *life) create(a map[string]any) (any, error) {
	start, err := parseWhen(str(a, "start"), str(a, "timezone"))
	if err != nil {
		return nil, err
	}
	end, err := parseWhen(str(a, "end"), str(a, "timezone"))
	if err != nil {
		return nil, err
	}
	if !end.After(start) {
		return nil, errors.New("end must be after start")
	}
	status := str(a, "status")
	if status == "" {
		status = "confirmed"
	}
	e := Event{ID: fmt.Sprintf("ev%02d", len(l.events)+1), Title: str(a, "title"), Start: start, End: end, Status: status, Added: true}
	l.events = append(l.events, e)
	return describe(e), nil
}

func (l *life) update(a map[string]any) (any, error) {
	for n := range l.events {
		e := &l.events[n]
		if e.ID != str(a, "id") || e.Gone {
			continue
		}
		if title := str(a, "title"); title != "" {
			e.Title = title
		}
		for key, target := range map[string]*time.Time{"start": &e.Start, "end": &e.End} {
			if value := str(a, key); value != "" {
				t, err := parseWhen(value, str(a, "timezone"))
				if err != nil {
					return nil, err
				}
				*target = t
			}
		}
		if status := str(a, "status"); status != "" {
			e.Status = status
		}
		return describe(*e), nil
	}
	return nil, errors.New("no event with that id")
}

func (l *life) remove(id string) (any, error) {
	for n := range l.events {
		if l.events[n].ID == id && !l.events[n].Gone {
			l.events[n].Gone = true
			return map[string]any{"deleted": true}, nil
		}
	}
	return nil, errors.New("no event with that id")
}

// clash reports a live event other than skip that overlaps start to end.
func (l *life) clash(start, end time.Time, skip string) *Event {
	for n := range l.events {
		e := &l.events[n]
		if !e.Gone && e.ID != skip && e.Start.Before(end) && start.Before(e.End) {
			return e
		}
	}
	return nil
}

// clashBefore reports an event the user already had that overlaps start to end.
func (l *life) clashBefore(start, end time.Time) *Event {
	for n := range l.events {
		e := &l.events[n]
		if !e.Gone && !e.Added && e.Start.Before(end) && start.Before(e.End) {
			return e
		}
	}
	return nil
}

// added lists live events made during the run whose title matches test.
func (l *life) added(test func(string) bool) []Event {
	var out []Event
	for _, e := range l.events {
		if e.Added && !e.Gone && test(e.Title) {
			out = append(out, e)
		}
	}
	return out
}
