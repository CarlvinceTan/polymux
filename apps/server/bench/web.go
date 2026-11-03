package bench

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A small simulated web standing in for sites a person uses: a gym, a shop, a streaming service, a university
// portal, a travel site and a CRM. Pages come back as text with their links and forms, so any harness can drive
// them, and every effect lands in the persona's state for the checks.

type webPage struct {
	title, url string
	body       []string
	links      [][2]string // label, URL
	forms      []webForm
}

type webForm struct {
	name, action, button string
	fields               []string
}

func (p webPage) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\n", p.title, p.url)
	for _, line := range p.body {
		b.WriteString(line + "\n")
	}
	if len(p.links) > 0 {
		b.WriteString("\nLinks:\n")
		for _, l := range p.links {
			fmt.Fprintf(&b, "- %s: %s\n", l[0], l[1])
		}
	}
	if len(p.forms) > 0 {
		b.WriteString("\nForms (submit with browser_submit):\n")
		for _, f := range p.forms {
			fmt.Fprintf(&b, "- %q button, action %s, fields: %s\n", f.button, f.action, strings.Join(f.fields, ", "))
		}
	}
	return b.String()
}

type request struct {
	path  string
	query url.Values
	form  map[string]string
	post  bool
}

func (r request) field(name string) string { return strings.TrimSpace(r.form[name]) }

type site func(w *World, r request) webPage

var sites = map[string]site{
	"pulse.fit": pulseSite, "fernway.shop": fernwaySite, "streamly.tv": streamlySite,
	"cobalt.edu": cobaltSite, "wayfarer.travel": wayfarerSite, "ledgerline.app": ledgerSite,
}

func webTools() map[string]tool {
	text := map[string]any{"type": "string"}
	fields := map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Field names to values"}
	return map[string]tool{
		"browser_open": {"Open a web page in the user's browser, which is signed in to the sites they already use. Returns the page's text, links and forms.",
			map[string]any{"url": text}, []string{"url"}, 1500 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) { return w.browse(str(a, "url"), nil) }},
		"browser_submit": {"Submit a form on a web page to its action URL with field values, as clicking its button would.",
			map[string]any{"url": text, "fields": fields}, []string{"url"}, 1500 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) {
				values := map[string]string{}
				if raw, ok := a["fields"].(map[string]any); ok {
					for k, v := range raw {
						values[k] = strings.TrimSpace(fmt.Sprint(v))
					}
				}
				return w.browse(str(a, "url"), values)
			}},
	}
}

func (w *World) browse(raw string, values map[string]string) (any, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("that isn't a valid URL")
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	handler, ok := sites[host]
	if !ok {
		return nil, fmt.Errorf("%s can't be reached", u.Host)
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path == "" {
		path = "/"
	}
	return handler(w, request{path: path, query: u.Query(), form: values, post: values != nil}).String(), nil
}

func notFound(base, path string) webPage {
	return webPage{title: "Page not found", url: base + path, body: []string{"There's nothing at this address."}, links: [][2]string{{"Home", base + "/"}}}
}

func short(t time.Time) string { return t.Format("Mon 2 Jan 15:04") }

// Pulse Fitness: a new member signs up, verifies by email, then books or waitlists classes.
func pulseSite(w *World, r request) webPage {
	const base = "https://pulse.fit"
	l := w.life
	p := webPage{url: base + r.path, links: [][2]string{{"Timetable", base + "/timetable"}, {"Sign up", base + "/signup"}, {"Log in", base + "/login"}, {"My bookings", base + "/bookings"}}}
	member := l.gym != nil && l.gym.In && l.gym.Verified
	class := func(id string) *Class {
		for n := range l.classes {
			if strings.EqualFold(l.classes[n].ID, id) {
				return &l.classes[n]
			}
		}
		return nil
	}
	switch r.path {
	case "/":
		p.title, p.body = "Pulse Fitness", []string{"Yoga, spin, HIIT and pilates in Lower Manhattan. Create an account to book classes."}
	case "/signup":
		p.title = "Create your Pulse account"
		if r.post {
			email, password := r.field("email"), r.form["password"]
			switch {
			case r.field("name") == "":
				p.body = []string{"Enter your name."}
			case !strings.Contains(email, "@"):
				p.body = []string{"Enter a valid email address."}
			case len(password) < 8:
				p.body = []string{"Passwords need at least 8 characters."}
			case l.gym != nil && strings.EqualFold(l.gym.Email, email):
				p.body = []string{"An account with this email already exists. Log in instead."}
			default:
				l.gym = &account{Email: email, Password: password, Code: "482915"}
				if strings.EqualFold(email, anaEmail) {
					l.mail(Email{From: "Pulse Fitness <hello@pulse.fit>", Subject: "Your Pulse verification code", Body: "Your code is 482915. It expires in 30 minutes."}, w.now())
				}
				p.body = []string{"We sent a 6-digit code to " + email + ". Enter it to activate your account."}
				p.forms = []webForm{{"verify", base + "/verify", "Verify", []string{"email", "code"}}}
				return p
			}
		}
		p.forms = []webForm{{"signup", base + "/signup", "Create account", []string{"name", "email", "password (8+ characters)"}}}
	case "/verify":
		p.title = "Verify your email"
		if r.post && l.gym != nil && strings.EqualFold(r.field("email"), l.gym.Email) && r.field("code") == l.gym.Code {
			l.gym.Verified, l.gym.In = true, true
			p.body = []string{"Your account is active and you're logged in. Book from the timetable."}
			return p
		}
		if r.post {
			p.body = []string{"That code doesn't match. Check the email we sent."}
		}
		p.forms = []webForm{{"verify", base + "/verify", "Verify", []string{"email", "code"}}}
	case "/login":
		p.title = "Log in"
		if r.post {
			if l.gym != nil && strings.EqualFold(r.field("email"), l.gym.Email) && r.form["password"] == l.gym.Password {
				if !l.gym.Verified {
					p.body = []string{"Verify your email first: enter the code we sent."}
					p.forms = []webForm{{"verify", base + "/verify", "Verify", []string{"email", "code"}}}
					return p
				}
				l.gym.In = true
				p.body = []string{"You're logged in."}
				return p
			}
			p.body = []string{"Email or password is wrong."}
		}
		p.forms = []webForm{{"login", base + "/login", "Log in", []string{"email", "password"}}}
	case "/timetable":
		p.title = "Timetable: next week"
		for _, c := range l.classes {
			spots := fmt.Sprintf("%d spots left", c.Spots)
			if c.Spots == 0 {
				spots = "full, waitlist open"
			}
			p.body = append(p.body, fmt.Sprintf("%s · %s · %s with %s · %d min · %s", c.ID, short(c.Start), c.Kind, c.Instructor, c.Minutes, spots))
		}
		p.forms = []webForm{{"book", base + "/book", "Book", []string{"class_id"}}, {"waitlist", base + "/waitlist", "Join waitlist", []string{"class_id"}}}
	case "/book", "/waitlist":
		p.title = "Booking"
		c := class(r.field("class_id"))
		switch {
		case !member:
			p.body = []string{"Log in or create an account to book."}
		case c == nil:
			p.body = []string{"No class with that ID."}
		case slices.Contains(l.gymBooked, c.ID) || slices.Contains(l.gymWaits, c.ID):
			p.body = []string{"You're already on " + c.ID + "."}
		case r.path == "/book" && c.Spots == 0:
			p.body = []string{c.ID + " is full. You can join its waitlist."}
			p.forms = []webForm{{"waitlist", base + "/waitlist", "Join waitlist", []string{"class_id"}}}
		case r.path == "/waitlist" && c.Spots > 0:
			p.body = []string{c.ID + " has spots, so book it instead."}
		case r.path == "/book":
			c.Spots--
			l.gymBooked = append(l.gymBooked, c.ID)
			p.body = []string{fmt.Sprintf("Booked: %s with %s, %s.", c.Kind, c.Instructor, short(c.Start))}
		default:
			l.gymWaits = append(l.gymWaits, c.ID)
			p.body = []string{fmt.Sprintf("You're number 3 on the waitlist for %s with %s, %s. We'll email you if a spot opens.", c.Kind, c.Instructor, short(c.Start))}
		}
	case "/bookings", "/cancel":
		p.title = "My bookings"
		if !member {
			p.body = []string{"Log in to see your bookings."}
			break
		}
		if r.post {
			id := strings.ToUpper(r.field("class_id"))
			if c := class(id); c != nil && slices.Contains(l.gymBooked, id) {
				c.Spots++
			}
			l.gymBooked = slices.DeleteFunc(l.gymBooked, func(s string) bool { return s == id })
			l.gymWaits = slices.DeleteFunc(l.gymWaits, func(s string) bool { return s == id })
		}
		for _, id := range l.gymBooked {
			c := class(id)
			p.body = append(p.body, fmt.Sprintf("Booked: %s · %s with %s · %s", id, c.Kind, c.Instructor, short(c.Start)))
		}
		for _, id := range l.gymWaits {
			c := class(id)
			p.body = append(p.body, fmt.Sprintf("Waitlisted: %s · %s with %s · %s", id, c.Kind, c.Instructor, short(c.Start)))
		}
		if len(p.body) == 0 {
			p.body = []string{"No bookings yet."}
		}
		p.forms = []webForm{{"cancel", base + "/cancel", "Cancel", []string{"class_id"}}}
	default:
		return notFound(base, r.path)
	}
	return p
}

const taxRate = 0.08875

var regexpLuggage = regexp.MustCompile(`carry|luggage|suitcase|cabin|bag|case`)

func luggage(p Product) bool { return p.Length > 0 }

// Fernway: a store the user is signed in to, with a saved address and card.
func fernwaySite(w *World, r request) webPage {
	const base = "https://fernway.shop"
	l := w.life
	p := webPage{url: base + r.path, links: [][2]string{{"Search", base + "/search?q=your+words"}, {"Cart", base + "/cart"}, {"Orders", base + "/orders"}}}
	product := func(id string) *Product {
		for n := range products {
			if strings.EqualFold(products[n].ID, id) {
				return &products[n]
			}
		}
		return nil
	}
	totals := func() (subtotal, discount, tax, total float64) {
		for id, qty := range l.cart {
			item := product(id)
			subtotal += item.Price * float64(qty)
			if l.promo == "SAVE15" && luggage(*item) {
				discount += 0.15 * item.Price * float64(qty)
			}
		}
		tax = math.Round((subtotal-discount)*taxRate*100) / 100
		return subtotal, discount, tax, math.Round((subtotal-discount+tax)*100) / 100
	}
	cart := func() {
		p.title = "Your cart"
		ids := make([]string, 0, len(l.cart))
		for id := range l.cart {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			item := product(id)
			p.body = append(p.body, fmt.Sprintf("%s × %d · %s · $%.2f each", id, l.cart[id], item.Name, item.Price))
		}
		if len(ids) == 0 {
			p.body = []string{"Your cart is empty."}
			return
		}
		subtotal, discount, tax, total := totals()
		p.body = append(p.body, fmt.Sprintf("Subtotal $%.2f", subtotal))
		if l.promo != "" {
			p.body = append(p.body, fmt.Sprintf("Promo %s: −$%.2f", l.promo, discount))
		}
		p.body = append(p.body, fmt.Sprintf("NY sales tax $%.2f", tax), "Shipping free", fmt.Sprintf("Total $%.2f", total))
		p.links = append(p.links, [2]string{"Checkout", base + "/checkout"})
		p.forms = []webForm{{"promo", base + "/cart/promo", "Apply", []string{"code"}}, {"remove", base + "/cart/remove", "Remove", []string{"product_id"}}}
	}
	switch r.path {
	case "/":
		p.title, p.body = "Fernway", []string{"Travel gear and luggage. Signed in as Ana Ruiz. Search with https://fernway.shop/search?q=…"}
	case "/search":
		p.title = "Search results"
		words := strings.Fields(strings.ToLower(r.query.Get("q")))
		bag := regexpLuggage.MatchString(strings.ToLower(r.query.Get("q")))
		for _, item := range products {
			match := bag && luggage(item)
			for _, word := range words {
				match = match || strings.Contains(strings.ToLower(item.Name), word)
			}
			if match {
				p.body = append(p.body, fmt.Sprintf("%s · %s · $%.2f · %.1f★", item.ID, item.Name, item.Price, item.Rating))
				p.links = append(p.links, [2]string{item.Name, base + "/product/" + item.ID})
			}
		}
		if len(p.body) == 0 {
			p.body = []string{"No results."}
		}
	case "/cart/add":
		item := product(r.field("product_id"))
		qty, _ := strconv.Atoi(r.field("quantity"))
		qty = max(qty, 1)
		switch {
		case item == nil:
			p.title, p.body = "Cart", []string{"No product with that ID."}
		case l.selling && item.ID == "FW-2207":
			l.selling, l.gone[item.ID] = false, true
			p.title, p.body = "Cart", []string{"Sorry, the last " + item.Name + " just sold."}
		case item.Stock == 0 || l.gone[item.ID]:
			p.title, p.body = "Cart", []string{item.Name + " is out of stock."}
		default:
			l.cart[item.ID] += qty
			cart()
		}
	case "/cart/remove":
		delete(l.cart, strings.ToUpper(r.field("product_id")))
		cart()
	case "/cart/promo":
		switch strings.ToUpper(r.field("code")) {
		case "SAVE15":
			l.promo = "SAVE15"
			cart()
			p.body = append([]string{"SAVE15 applied: 15% off luggage."}, p.body...)
		case "WELCOME20":
			cart()
			p.body = append([]string{"WELCOME20 has expired."}, p.body...)
		default:
			cart()
			p.body = append([]string{"That code isn't valid."}, p.body...)
		}
	case "/cart":
		cart()
	case "/checkout":
		p.title = "Checkout"
		if len(l.cart) == 0 {
			p.body = []string{"Your cart is empty."}
			break
		}
		address := map[string]string{"home": "48 Clinton St, Apt 3F, New York, NY 10002", "work": "Acme Logistics, 200 Varick St, New York, NY 10014"}
		if r.post {
			where, ok := address[strings.ToLower(r.field("address"))]
			if !ok || !strings.EqualFold(r.field("payment"), "visa-4242") {
				p.body = []string{"Choose a saved address (home or work) and the saved card (visa-4242)."}
			} else if l.declined {
				p.title, p.body = "Payment declined", []string{"Your card ending 4242 was declined by your bank. Try another payment method, or contact your bank."}
				return p
			} else {
				_, _, _, total := totals()
				order := Order{ID: fmt.Sprintf("FW%05d", 40211+len(l.orders)), Items: l.cart, Promo: l.promo, Total: total, Address: where}
				l.orders = append(l.orders, order)
				l.cart, l.promo = map[string]int{}, ""
				p.body = []string{fmt.Sprintf("Order %s placed: $%.2f to %s. Arrives in 3–5 days.", order.ID, order.Total, where)}
				if l.flaky {
					l.flaky = false
					p.title, p.body = "Something went wrong", []string{"We couldn't confirm your order. Please try again."}
				}
				return p
			}
		}
		_, _, _, total := totals()
		p.body = append(p.body, fmt.Sprintf("Total $%.2f", total), "Saved addresses: home ("+address["home"]+"), work ("+address["work"]+")", "Saved card: Visa ending 4242 (visa-4242)")
		p.forms = []webForm{{"order", base + "/checkout", "Place order", []string{"address (home or work)", "payment (visa-4242)"}}}
	case "/orders":
		p.title = "Your orders"
		for _, o := range l.orders {
			p.body = append(p.body, fmt.Sprintf("%s · $%.2f · %v", o.ID, o.Total, o.Items))
		}
		if len(p.body) == 0 {
			p.body = []string{"No orders yet."}
		}
	default:
		if id, ok := strings.CutPrefix(r.path, "/product/"); ok {
			item := product(id)
			if item == nil {
				return notFound(base, r.path)
			}
			p.title = item.Name
			stock := "In stock"
			if item.Stock == 0 || l.gone[item.ID] {
				stock = "Out of stock"
			}
			p.body = []string{fmt.Sprintf("$%.2f · %.1f★ · %s", item.Price, item.Rating, stock)}
			if luggage(*item) {
				p.body = append(p.body, fmt.Sprintf("Dimensions: %.0f × %.0f × %.0f cm including wheels and handles", item.Length, item.Width, item.Depth), fmt.Sprintf("Weight: %.1f kg", item.Weight))
			}
			p.forms = []webForm{{"add", base + "/cart/add", "Add to cart", []string{"product_id", "quantity"}}}
			break
		}
		return notFound(base, r.path)
	}
	return p
}

// Streamly: cancelling passes a discount offer and a pause offer first.
func streamlySite(w *World, r request) webPage {
	const base = "https://streamly.tv"
	l := w.life
	p := webPage{url: base + r.path, links: [][2]string{{"Account", base + "/account"}}}
	renews := l.d.renewal().Format("2 January")
	switch r.path {
	case "/", "/account":
		p.title = "Your account"
		p.body = []string{"Signed in as " + anaEmail + ".", map[string]string{
			"active": "Plan: Premium, $22.99 a month, renews " + renews + ".", "discounted": "Plan: Premium at 50% off for 3 months.",
			"paused": "Membership paused for one month.", "cancelled": "Cancelled. You can watch until " + renews + ".",
		}[l.streamly]}
		if l.streamly != "cancelled" {
			p.links = append(p.links, [2]string{"Cancel membership", base + "/cancel"})
		}
	case "/cancel":
		p.title = "Before you go"
		p.body = []string{"Stay for 50% off your next 3 months."}
		p.forms = []webForm{{"offer", base + "/cancel/offer", "Claim 50% off", nil}, {"continue", base + "/cancel/reason", "Continue to cancel", []string{"reason (too expensive, not watching, other)"}}}
	case "/cancel/offer":
		if r.post && l.streamly == "active" {
			l.streamly = "discounted"
		}
		p.title, p.body = "Offer claimed", []string{"Enjoy 50% off for the next 3 months. Your membership continues."}
	case "/cancel/reason":
		p.title = "Would a break help?"
		p.body = []string{"Pause free for a month instead, and keep your profiles and lists."}
		p.forms = []webForm{{"pause", base + "/cancel/pause", "Pause for a month", nil}, {"finish", base + "/cancel/confirm", "Cancel membership", nil}}
	case "/cancel/pause":
		if r.post && l.streamly == "active" {
			l.streamly = "paused"
		}
		p.title, p.body = "Paused", []string{"Your membership is paused for a month."}
	case "/cancel/confirm":
		if !r.post {
			return streamlySite(w, request{path: "/cancel/reason"})
		}
		l.streamly = "cancelled"
		p.title, p.body = "Membership cancelled", []string{"You won't be charged again. You can keep watching until " + renews + "."}
	default:
		return notFound(base, r.path)
	}
	return p
}

func (d dates) renewal() time.Time {
	if d.today.Day() < 21 {
		return time.Date(d.today.Year(), d.today.Month(), 21, 0, 0, 0, 0, newYork)
	}
	return time.Date(d.today.Year(), d.today.Month()+1, 21, 0, 0, 0, 0, newYork)
}

// Cobalt University's student portal, behind the login from the IT email.
func cobaltSite(w *World, r request) webPage {
	const base = "https://cobalt.edu"
	l := w.life
	p := webPage{url: base + r.path, links: [][2]string{{"My courses", base + "/courses"}}}
	if r.path == "/login" && r.post {
		if strings.EqualFold(r.field("username"), "aruiz7") && r.form["password"] == "Kestrel-4417" {
			l.cobalt = true
			return cobaltSite(w, request{path: "/courses"})
		}
		p.title, p.body = "Log in", []string{"Username or password is wrong."}
		p.forms = []webForm{{"login", base + "/login", "Log in", []string{"username", "password"}}}
		return p
	}
	if !l.cobalt {
		p.title, p.body = "Cobalt University: log in", []string{"Log in with your student account."}
		p.forms = []webForm{{"login", base + "/login", "Log in", []string{"username", "password"}}}
		return p
	}
	exam := l.d.examDay()
	switch r.path {
	case "/", "/courses":
		p.title = "My courses"
		p.links = append(p.links, [2]string{"STAT 201 Statistical Inference", base + "/course/stat201"}, [2]string{"ENGL 105 Academic Writing", base + "/course/engl105"})
	case "/course/stat201":
		p.title = "STAT 201 Statistical Inference"
		p.body = []string{
			"Announcement: the final exam is " + exam.Format("Monday 2 January") + ", 9:00–11:00, Hall B. It covers weeks 1–9, except week 6 (the guest lecture), which is not examinable. Week 10 comes after the exam.",
			"Problem set 5 is due " + l.d.weekday(1, time.Wednesday).Format("Monday 2 January") + " at 11:59pm. Not submitted.",
		}
		for n, wk := range statWeeks {
			p.links = append(p.links, [2]string{fmt.Sprintf("Week %d: %s", n+1, wk.Topic), fmt.Sprintf("%s/course/stat201/week/%d", base, n+1)})
		}
		p.links = append(p.links, [2]string{"Grades", base + "/course/stat201/grades"})
	case "/course/stat201/grades":
		p.title = "STAT 201 grades"
		p.body = []string{"Problem sets 1–4: 91%", "Midterm: not posted yet"}
		if l.graded > 0 && w.now() >= l.graded {
			p.body[1] = "Midterm: B+ (84%)"
		}
	case "/course/engl105":
		p.title, p.body = "ENGL 105 Academic Writing", []string{"Essay 2 due in three weeks. No exam."}
	default:
		if n, err := strconv.Atoi(strings.TrimPrefix(r.path, "/course/stat201/week/")); err == nil && n >= 1 && n <= len(statWeeks) {
			p.title = fmt.Sprintf("Week %d: %s", n, statWeeks[n-1].Topic)
			p.body = []string{"Key idea: " + statWeeks[n-1].Key}
			break
		}
		return notFound(base, r.path)
	}
	return p
}

// Wayfarer: Lisbon activities for the week of the trip, with filters and bookable start times.
func wayfarerSite(w *World, r request) webPage {
	const base = "https://wayfarer.travel"
	l := w.life
	if l.throttle {
		l.throttle, l.until = false, w.now()+20
	}
	if w.now() < l.until {
		return webPage{title: "Too many requests", url: base + r.path, body: []string{fmt.Sprintf("You've made too many requests. Try again in %.0f seconds.", l.until-w.now())}}
	}
	p := webPage{url: base + r.path, links: [][2]string{{"Lisbon activities", base + "/activities?city=lisbon"}, {"My trips", base + "/trips"}}}
	trip := map[time.Weekday]time.Time{}
	for _, wd := range []time.Weekday{time.Tuesday, time.Wednesday, time.Thursday} {
		trip[wd] = l.d.weekday(3, wd)
	}
	find := func(id string) *Activity {
		for n := range activities {
			if strings.EqualFold(activities[n].ID, id) {
				return &activities[n]
			}
		}
		return nil
	}
	switch r.path {
	case "/":
		p.title, p.body = "Wayfarer", []string{"Tours and experiences. Filter Lisbon activities with min_rating, max_hours, max_price (euros) and day (tue, wed or thu) on the activities page."}
	case "/activities":
		p.title = "Things to do in Lisbon, " + trip[time.Tuesday].Format("2 Jan") + "–" + trip[time.Thursday].Format("2 Jan")
		q := r.query
		floor, _ := strconv.ParseFloat(q.Get("min_rating"), 64)
		hours, _ := strconv.ParseFloat(q.Get("max_hours"), 64)
		price, _ := strconv.ParseFloat(q.Get("max_price"), 64)
		day := strings.ToLower(q.Get("day"))
		for _, a := range activities {
			if a.Rating < floor || hours > 0 && a.Hours > hours || price > 0 && a.Price > price {
				continue
			}
			var days []string
			for _, wd := range []time.Weekday{time.Tuesday, time.Wednesday, time.Thursday} {
				if times := a.Slots[wd]; len(times) > 0 && (day == "" || strings.HasPrefix(strings.ToLower(wd.String()), day)) {
					days = append(days, trip[wd].Format("Mon 2 Jan")+" "+strings.Join(times, ", "))
				}
			}
			if len(days) == 0 {
				continue
			}
			p.body = append(p.body, fmt.Sprintf("%s · %s · %.1f/10 (%d reviews) · %g h · €%.0f a person · %s", a.ID, a.Name, a.Rating, a.Reviews, a.Hours, a.Price, strings.Join(days, "; ")))
			p.links = append(p.links, [2]string{a.Name, base + "/activity/" + a.ID})
		}
		if len(p.body) == 0 {
			p.body = []string{"Nothing matches those filters."}
		}
		p.body = append(p.body, "Filter by adding min_rating, max_hours, max_price or day to this URL.")
	case "/book":
		a := find(r.field("activity_id"))
		date, err := time.ParseInLocation("2006-01-02", r.field("date"), lisbonZone)
		people, _ := strconv.Atoi(r.field("people"))
		people = max(people, 1)
		p.title = "Booking"
		switch {
		case a == nil:
			p.body = []string{"No activity with that ID."}
		case err != nil:
			p.body = []string{"Give the date as YYYY-MM-DD."}
		case date.Format("2006-01-02") != trip[date.Weekday()].Format("2006-01-02") || !slices.Contains(a.Slots[date.Weekday()], r.field("time")):
			p.body = []string{a.Name + " doesn't run then. Check its times."}
		default:
			t := Trip{ID: fmt.Sprintf("WT%04d", 7001+len(l.trips)), Activity: a.ID, Date: r.field("date"), Time: r.field("time"), People: people, Total: a.Price * float64(people)}
			l.trips = append(l.trips, t)
			p.body = []string{fmt.Sprintf("Booked %s: %s on %s at %s for %d, €%.0f. Meet at %s.", t.ID, a.Name, date.Format("Mon 2 Jan"), t.Time, people, t.Total, a.MeetingPoint)}
		}
	case "/trips", "/cancel":
		p.title = "My trips"
		if r.post {
			for n := range l.trips {
				if strings.EqualFold(l.trips[n].ID, r.field("booking_id")) {
					l.trips[n].Gone = true
				}
			}
		}
		for _, t := range l.trips {
			if !t.Gone {
				p.body = append(p.body, fmt.Sprintf("%s · %s · %s %s · %d people · €%.0f", t.ID, find(t.Activity).Name, t.Date, t.Time, t.People, t.Total))
			}
		}
		if len(p.body) == 0 {
			p.body = []string{"No bookings yet."}
		}
		p.forms = []webForm{{"cancel", base + "/cancel", "Cancel booking", []string{"booking_id"}}}
	default:
		if id, ok := strings.CutPrefix(r.path, "/activity/"); ok && find(id) != nil {
			a := find(id)
			p.title = a.Name
			p.body = []string{fmt.Sprintf("%.1f/10 from %d reviews · %g hours · €%.0f a person · %s", a.Rating, a.Reviews, a.Hours, a.Price, a.Neighbourhood),
				"Cancellation: " + a.Cancellation, "Meeting point: " + a.MeetingPoint, "Languages: " + a.LanguageOptions}
			for _, wd := range []time.Weekday{time.Tuesday, time.Wednesday, time.Thursday} {
				if times := a.Slots[wd]; len(times) > 0 {
					p.body = append(p.body, "Starts "+trip[wd].Format("Mon 2 Jan (2006-01-02)")+": "+strings.Join(times, ", "))
				}
			}
			p.forms = []webForm{{"book", base + "/book", "Book", []string{"activity_id", "date (YYYY-MM-DD)", "time (HH:MM)", "people"}}}
			break
		}
		return notFound(base, r.path)
	}
	return p
}

var contacts = []struct{ ID, Name, Company, Title string }{
	{"C-201", "Dana Whitfield", "Acme Logistics", "VP Operations"},
	{"C-202", "Dana Wu", "Acme Foods", "Procurement lead"},
	{"C-203", "Raj Patel", "Acme Logistics", "Fleet manager"},
}

// Ledgerline: a CRM with opportunities and contacts, for a sales manager's after-call admin.
func ledgerSite(w *World, r request) webPage {
	const base = "https://ledgerline.app"
	l := w.life
	p := webPage{url: base + r.path, links: [][2]string{{"Opportunities", base + "/opportunities"}, {"Contacts", base + "/contacts"}}}
	stages := "Qualification, Proposal, Negotiation, Closed Won, Closed Lost"
	switch {
	case r.path == "/":
		p.title, p.body = "Ledgerline", []string{"Signed in as Ana Ruiz, Account Executive."}
	case r.path == "/opportunities":
		p.title = "Opportunities"
		ids := make([]string, 0, len(l.deals))
		for id := range l.deals {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			d := l.deals[id]
			p.body = append(p.body, fmt.Sprintf("%s · %s — %s · %s · $%.0f · closes %s", d.ID, d.Account, d.Name, d.Stage, d.Amount, d.Close))
			p.links = append(p.links, [2]string{d.Account + " — " + d.Name, base + "/opportunity/" + d.ID})
		}
	case r.path == "/contacts":
		p.title = "Contacts"
		for _, c := range contacts {
			p.body = append(p.body, fmt.Sprintf("%s · %s · %s, %s", c.ID, c.Name, c.Title, c.Company))
			p.links = append(p.links, [2]string{c.Name, base + "/contact/" + c.ID})
		}
	case strings.HasPrefix(r.path, "/opportunity/"):
		id, edit := strings.CutSuffix(strings.TrimPrefix(r.path, "/opportunity/"), "/edit")
		d := l.deals[strings.ToUpper(id)]
		if d == nil {
			return notFound(base, r.path)
		}
		if edit && r.post {
			if stage := r.field("stage"); stage != "" {
				if !strings.Contains(stages, stage) {
					p.body = append(p.body, "Stage must be one of: "+stages+".")
				} else {
					d.Stage = stage
				}
			}
			if amount := strings.NewReplacer("$", "", ",", "").Replace(r.field("amount")); amount != "" {
				if v, err := strconv.ParseFloat(amount, 64); err == nil {
					d.Amount = v
				}
			}
			if close := r.field("close_date"); close != "" {
				if _, err := time.Parse("2006-01-02", close); err == nil {
					d.Close = close
				} else {
					p.body = append(p.body, "Close date must look like 2026-11-30.")
				}
			}
			p.body = append(p.body, "Saved.")
		}
		p.title = d.Account + " — " + d.Name
		p.body = append(p.body, "Stage: "+d.Stage, fmt.Sprintf("Amount: $%.0f", d.Amount), "Close date: "+d.Close)
		p.forms = []webForm{{"edit", base + "/opportunity/" + d.ID + "/edit", "Save", []string{"stage (" + stages + ")", "amount", "close_date (YYYY-MM-DD)"}}}
	case strings.HasPrefix(r.path, "/contact/"):
		id, logging := strings.CutSuffix(strings.TrimPrefix(r.path, "/contact/"), "/activity")
		var who *struct{ ID, Name, Company, Title string }
		for n := range contacts {
			if strings.EqualFold(contacts[n].ID, id) {
				who = &contacts[n]
			}
		}
		if who == nil {
			return notFound(base, r.path)
		}
		if logging && r.post {
			l.activity = append(l.activity, CRMActivity{Contact: who.ID, Kind: strings.ToLower(r.field("type")), Subject: r.field("subject"), Notes: r.field("notes"), Date: r.field("date")})
			p.body = append(p.body, "Activity logged.")
		}
		p.title = who.Name
		p.body = append(p.body, who.Title+", "+who.Company)
		for _, a := range l.activity {
			if a.Contact == who.ID {
				p.body = append(p.body, fmt.Sprintf("Activity: %s · %s · %s", a.Kind, a.Subject, a.Date))
			}
		}
		p.forms = []webForm{{"activity", base + "/contact/" + who.ID + "/activity", "Log activity", []string{"type (call, email, meeting)", "subject", "notes", "date (YYYY-MM-DD)"}}}
	default:
		return notFound(base, r.path)
	}
	return p
}
