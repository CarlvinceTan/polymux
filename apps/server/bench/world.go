package bench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Booking is a table the world has reserved.
type Booking struct {
	Restaurant   string  `json:"restaurant"`
	Date         string  `json:"date"`
	Time         string  `json:"time"`
	Party        int     `json:"party_size"`
	Confirmation string  `json:"confirmation"`
	Status       string  `json:"status"`
	At           float64 `json:"at"`
}

// ToolCall is one tool use the world served.
type ToolCall struct {
	At   float64        `json:"at"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type tool struct {
	description string
	params      map[string]any
	required    []string
	delay       time.Duration
	run         func(w *World, args map[string]any) (any, error)
}

// World is one run's simulated environment, served to every system as an MCP server.
type World struct {
	Project string // the buggy project the coding scenarios point at
	URL     string

	start    time.Time
	listener net.Listener
	server   *http.Server
	tools    map[string]tool
	testHash string

	life    *life
	state   sync.Mutex // serialises tool effects; their delays still overlap
	instant bool       // skips tool delays, for tests

	mu       sync.Mutex
	bookings []Booking
	calls    []ToolCall
	requests []string
	inflight int
}

// NewWorld prepares a world rooted in dir.
func NewWorld(dir string, start time.Time) (*World, error) {
	w := &World{Project: filepath.Join(dir, "project"), start: start}
	if err := os.MkdirAll(w.Project, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(w.Project, "pricing.py"), []byte(pricingPy), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(w.Project, "test_pricing.py"), []byte(testPy), 0o600); err != nil {
		return nil, err
	}
	var csv strings.Builder
	csv.WriteString("year,cash_flow\n")
	for year, flow := range rental {
		fmt.Fprintf(&csv, "%d,%.0f\n", year, flow)
	}
	if err := os.WriteFile(filepath.Join(w.Project, "rental.csv"), []byte(csv.String()), 0o600); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(testPy))
	w.testHash = hex.EncodeToString(sum[:])
	w.life = newLife(start, w.now)
	w.tools = worldTools()
	for _, more := range []map[string]tool{officeTools(), webTools()} {
		for name, t := range more {
			w.tools[name] = t
		}
	}
	return w, nil
}

// roles give specialist bots their own tools, each set served at /mcp/<role>.
var roles = map[string][]string{
	"finance": {"bank_balance", "bank_transactions", "bank_upcoming", "exchange_rate", "email_search", "email_read"},
	"travel":  {"web_search", "fetch_page", "search_restaurants", "book_table", "cancel_booking", "exchange_rate", "browser_open", "browser_submit", "email_search", "email_read"},
	"study":   {"browser_open", "browser_submit", "calendar_list", "calendar_create", "calendar_update", "calendar_delete", "email_search", "email_read", "web_search", "fetch_page"},
	"lead":    {"email_search", "email_read", "email_send", "calendar_list", "calendar_create", "calendar_update", "calendar_delete", "web_search", "fetch_page", "search_restaurants", "book_table", "cancel_booking"},
}

// RoleURL is the endpoint serving one role's tools.
func (w *World) RoleURL(role string) string { return w.URL + "/" + role }

// Mail delivers an email to the persona's inbox now.
func (w *World) Mail(e Email) {
	w.state.Lock()
	defer w.state.Unlock()
	w.life.mail(e, w.now())
}

// Life runs fn with the persona's state, for setup and checks.
func (w *World) Life(fn func(l *life)) {
	w.state.Lock()
	defer w.state.Unlock()
	fn(w.life)
}

// State is the persona's state at the end of a run, for the record.
func (w *World) State() map[string]any {
	w.state.Lock()
	defer w.state.Unlock()
	l := w.life
	var added []Event
	for _, e := range l.events {
		if e.Added || e.Gone {
			added = append(added, e)
		}
	}
	return map[string]any{"sent": l.sent, "events": added, "gym": l.gym, "gymBooked": l.gymBooked, "gymWaits": l.gymWaits, "orders": l.orders,
		"cart": l.cart, "streamly": l.streamly, "cobalt": l.cobalt, "trips": l.trips, "deals": l.deals, "activity": l.activity}
}

// Start serves the world on a loopback port.
func (w *World) Start() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("world listen: %w", err)
	}
	w.listener, w.URL = listener, "http://"+listener.Addr().String()+"/mcp"
	w.server = &http.Server{Handler: w, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = w.server.Serve(listener) }()
	return nil
}

// Close stops serving.
func (w *World) Close() { _ = w.server.Close() }

func (w *World) now() float64 { return time.Since(w.start).Seconds() }

// Bookings returns every booking made, cancelled ones included.
func (w *World) Bookings() []Booking {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Booking(nil), w.bookings...)
}

// Calls returns the tool calls served so far.
func (w *World) Calls() []ToolCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]ToolCall(nil), w.calls...)
}

// Busy reports a tool call in flight.
func (w *World) Busy() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.inflight > 0
}

// TestsPass runs the project's tests in a sandbox and reports whether they pass with the tests untouched.
func (w *World) TestsPass() bool {
	code, _ := sandboxedTests(w.Project)
	data, err := os.ReadFile(filepath.Join(w.Project, "test_pricing.py"))
	if err != nil {
		return false
	}
	sum := sha256.Sum256(data)
	return code == 0 && hex.EncodeToString(sum[:]) == w.testHash
}

type rpc struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Requests lists every MCP request received, for diagnosing clients.
func (w *World) Requests() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.requests...)
}

func (w *World) note(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.requests = append(w.requests, fmt.Sprintf("%.2f %s", w.now(), text))
}

func (w *World) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w.note(r.Method)
	if r.Method != http.MethodPost {
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var msg rpc
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		writeRPC(rw, nil, nil, &rpcError{-32700, "parse error"})
		return
	}
	w.note("  " + msg.Method)
	if len(msg.ID) == 0 {
		rw.WriteHeader(http.StatusAccepted)
		return
	}
	switch msg.Method {
	case "initialize":
		var params struct {
			Version string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		if params.Version == "" {
			params.Version = "2025-06-18"
		}
		rw.Header().Set("Mcp-Session-Id", "world")
		writeRPC(rw, msg.ID, map[string]any{"protocolVersion": params.Version, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{"name": "polymux-bench-world", "version": "0.1"}}, nil)
	case "ping":
		writeRPC(rw, msg.ID, map[string]any{}, nil)
	case "tools/list":
		writeRPC(rw, msg.ID, map[string]any{"tools": w.list(allowed(r.URL.Path))}, nil)
	case "tools/call":
		var params struct {
			Name string         `json:"name"`
			Args map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		if only := allowed(r.URL.Path); only != nil && !only[params.Name] {
			writeRPC(rw, msg.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "unknown tool " + params.Name}}, "isError": true}, nil)
			return
		}
		text, failed := w.call(r.Context(), params.Name, params.Args)
		writeRPC(rw, msg.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": failed}, nil)
	default:
		writeRPC(rw, msg.ID, nil, &rpcError{-32601, "method not found"})
	}
}

// allowed is the tool set a path serves, or nil for every tool.
func allowed(path string) map[string]bool {
	role, ok := strings.CutPrefix(path, "/mcp/")
	if !ok {
		return nil
	}
	set := map[string]bool{}
	for _, name := range roles[role] {
		set[name] = true
	}
	return set
}

func (w *World) list(only map[string]bool) []map[string]any {
	names := make([]string, 0, len(w.tools))
	for name := range w.tools {
		if only == nil || only[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		t := w.tools[name]
		required := t.required
		if required == nil {
			required = []string{}
		}
		out = append(out, map[string]any{"name": name, "description": t.description,
			"inputSchema": map[string]any{"type": "object", "properties": t.params, "required": required}})
	}
	return out
}

func (w *World) call(ctx context.Context, name string, args map[string]any) (string, bool) {
	t, ok := w.tools[name]
	if !ok {
		return "unknown tool " + name, true
	}
	if args == nil {
		args = map[string]any{}
	}
	w.mu.Lock()
	w.calls = append(w.calls, ToolCall{At: w.now(), Name: name, Args: args})
	w.inflight++
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.inflight--
		w.mu.Unlock()
	}()
	if !w.instant {
		select {
		case <-time.After(t.delay):
		case <-ctx.Done():
			return "cancelled", true
		}
	}
	w.state.Lock()
	value, err := t.run(w, args)
	w.state.Unlock()
	if err != nil {
		return "error: " + err.Error(), true
	}
	if text, ok := value.(string); ok {
		return text, false
	}
	data, _ := json.Marshal(value)
	return string(data), false
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeRPC(rw http.ResponseWriter, id json.RawMessage, result any, failure *rpcError) {
	reply := map[string]any{"jsonrpc": "2.0", "id": id}
	if failure != nil {
		reply["error"] = failure
	} else {
		reply["result"] = result
	}
	writeJSON(rw, http.StatusOK, reply)
}

func str(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func num(args map[string]any, key string) int {
	switch value := args[key].(type) {
	case float64:
		return int(value)
	case string:
		var n int
		_, _ = fmt.Sscan(value, &n)
		return n
	}
	return 0
}

func (w *World) inside(path string) (string, error) {
	target := path
	if !filepath.IsAbs(path) {
		target = filepath.Join(w.Project, path)
	}
	target = filepath.Clean(target)
	if target != w.Project && !strings.HasPrefix(target, w.Project+string(filepath.Separator)) {
		return "", errors.New("path is outside the project")
	}
	return target, nil
}

func worldTools() map[string]tool {
	text := map[string]any{"type": "string"}
	integer := map[string]any{"type": "integer"}
	return map[string]tool{
		"web_search": {"Search the web. Returns titles, URLs and snippets.", map[string]any{"query": text}, []string{"query"}, 3 * time.Second,
			func(w *World, a map[string]any) (any, error) { return search(str(a, "query")), nil }},
		"fetch_page": {"Fetch a web page's text by URL.", map[string]any{"url": text}, []string{"url"}, 2500 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) { return page(str(a, "url")), nil }},
		"reading_shelf": {"Read the user's reading shelf, five books per page.", map[string]any{"page": integer}, []string{"page"}, 4 * time.Second,
			func(w *World, a map[string]any) (any, error) { return shelfPage(num(a, "page")), nil }},
		"search_restaurants": {"Find restaurants with availability.", map[string]any{"cuisine": text, "area": text, "date": text, "party_size": integer}, nil, 2 * time.Second,
			func(w *World, a map[string]any) (any, error) { return restaurants, nil }},
		"book_table": {"Book a restaurant table. This makes a real booking.", map[string]any{"restaurant": text, "date": text, "time": text, "party_size": integer},
			[]string{"restaurant", "date", "time", "party_size"}, 2 * time.Second, (*World).book},
		"cancel_booking": {"Cancel a restaurant booking by its confirmation code.", map[string]any{"confirmation": text}, []string{"confirmation"}, time.Second, (*World).cancel},
		"exchange_rate": {"Get the current exchange rate between two currencies.", map[string]any{"base": text, "quote": text}, []string{"base", "quote"}, time.Second,
			func(w *World, a map[string]any) (any, error) {
				switch strings.ToUpper(str(a, "base")) + "/" + strings.ToUpper(str(a, "quote")) {
				case "USD/AUD":
					return map[string]any{"base": "USD", "quote": "AUD", "rate": usdAUD}, nil
				case "AUD/USD":
					return map[string]any{"base": "AUD", "quote": "USD", "rate": 1 / usdAUD}, nil
				case "EUR/USD":
					return map[string]any{"base": "EUR", "quote": "USD", "rate": eurUSD}, nil
				case "USD/EUR":
					return map[string]any{"base": "USD", "quote": "EUR", "rate": 1 / eurUSD}, nil
				}
				return nil, errors.New("only USD, AUD and EUR are available")
			}},
		"export_photos": {"Export the user's photo library for a year to a destination drive. Takes about a minute.", map[string]any{"year": integer, "destination": text},
			[]string{"year"}, time.Minute, func(w *World, a map[string]any) (any, error) {
				return map[string]any{"exported": photosExported, "skipped_duplicates": 12, "year": num(a, "year"), "destination": str(a, "destination")}, nil
			}},
		"list_files": {"List files in a project directory.", map[string]any{"path": text}, nil, 200 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) {
				entries, err := os.ReadDir(w.Project)
				if err != nil {
					return nil, err
				}
				var names []string
				for _, e := range entries {
					names = append(names, e.Name())
				}
				return names, nil
			}},
		"read_file": {"Read a file.", map[string]any{"path": text}, []string{"path"}, 200 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) {
				target, err := w.inside(str(a, "path"))
				if err != nil {
					return nil, err
				}
				data, err := os.ReadFile(target)
				return string(data), err
			}},
		"write_file": {"Write a file, replacing its contents.", map[string]any{"path": text, "content": text}, []string{"path", "content"}, 200 * time.Millisecond,
			func(w *World, a map[string]any) (any, error) {
				target, err := w.inside(str(a, "path"))
				if err != nil {
					return nil, err
				}
				return "written", os.WriteFile(target, []byte(str(a, "content")), 0o600)
			}},
		"run_tests": {"Run the project's unit tests.", map[string]any{"path": text}, nil, 0,
			func(w *World, a map[string]any) (any, error) {
				code, output := sandboxedTests(w.Project)
				return map[string]any{"exit_code": code, "output": output}, nil
			}},
	}
}

func (w *World) book(args map[string]any) (any, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	b := Booking{Restaurant: str(args, "restaurant"), Date: str(args, "date"), Time: str(args, "time"), Party: num(args, "party_size"),
		Confirmation: fmt.Sprintf("BK%04d", len(w.bookings)+1), Status: "active", At: w.now()}
	w.bookings = append(w.bookings, b)
	return map[string]any{"confirmed": true, "confirmation": b.Confirmation, "restaurant": b.Restaurant, "date": b.Date, "time": b.Time, "party_size": b.Party}, nil
}

func (w *World) cancel(args map[string]any) (any, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.bookings {
		if w.bookings[i].Confirmation == str(args, "confirmation") && w.bookings[i].Status == "active" {
			w.bookings[i].Status = "cancelled"
			return map[string]any{"cancelled": true, "confirmation": w.bookings[i].Confirmation}, nil
		}
	}
	return nil, errors.New("no active booking with that confirmation")
}

// sandboxedTests runs the project's tests with no network and no writes outside the project.
func sandboxedTests(dir string) (int, string) {
	profile := fmt.Sprintf(`(version 1)(allow default)(deny network*)(deny file-write* (subpath "/Users"))(allow file-write* (subpath %q))`, dir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "sandbox-exec", "-p", profile, "python3", "-I", "-m", "unittest", "discover", "-s", dir, "-t", dir)
	command.Dir = dir
	output, err := command.CombinedOutput()
	code := 0
	if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		return -1, err.Error()
	}
	text := string(output)
	if len(text) > 3000 {
		text = text[len(text)-3000:]
	}
	return code, text
}
