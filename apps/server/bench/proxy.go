package bench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Prices are USD per million tokens. Scored cost always uses them, because a provider's reported price can change
// between runs (OpenCode Go's doubled between two of ours), which would make costs incomparable.
const (
	priceInput  = 0.15
	priceCached = 0.003
	priceOutput = 0.60
)

// Call is one metered model request.
type Call struct {
	At         time.Time     `json:"at"`
	Latency    time.Duration `json:"latency"`
	Prompt     int           `json:"prompt"`
	Cached     int           `json:"cached"`
	Completion int           `json:"completion"`
	Cost       float64       `json:"cost"`   // at the pinned prices, for scores
	Billed     float64       `json:"billed"` // what the provider reported, for the spend cap
	Status     int           `json:"status"`
	Asked      string        `json:"asked,omitempty"`
}

// Proxy sits between every system and the model. It pins the model and reasoning
// level, holds the key, meters each call per run and enforces the spend cap.
type Proxy struct {
	Upstream string // OpenAI-compatible base, such as https://opencode.ai/zen/go/v1
	Key      string
	Model    string
	Effort   string
	Cap      float64                       // USD for the whole suite; zero means no cap
	First    func(run string, body []byte) // receives each run's first request, to compare what harnesses send

	URL      string
	listener net.Listener
	server   *http.Server
	client   *http.Client

	mu        sync.Mutex
	calls     map[string][]Call
	inflight  map[string]int
	recorded  map[string]bool
	exhausted bool // the provider reported its usage limit
	spent     float64
}

// Start listens on a loopback port.
func (p *Proxy) Start() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("proxy listen: %w", err)
	}
	p.listener, p.URL = listener, "http://"+listener.Addr().String()
	p.client = &http.Client{Timeout: 10 * time.Minute}
	p.calls, p.inflight = map[string][]Call{}, map[string]int{}
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = p.server.Serve(listener) }()
	return nil
}

// Close stops the proxy.
func (p *Proxy) Close() { _ = p.server.Close() }

// Base is the model URL a run's system uses.
func (p *Proxy) Base(run string) string { return p.URL + "/r/" + run + "/v1" }

// Calls returns a run's metered calls.
func (p *Proxy) Calls(run string) []Call {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Call(nil), p.calls[run]...)
}

// Busy reports a run's model calls in flight.
func (p *Proxy) Busy(run string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inflight[run] > 0
}

// Spent is the suite's cost so far.
// Exhausted reports that the provider has refused calls for its usage limit.
func (p *Proxy) Exhausted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exhausted
}

// Failed counts a run's calls the provider refused or broke, which make its score meaningless.
func (p *Proxy) Failed(run string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.calls[run] {
		if c.Status == http.StatusTooManyRequests || c.Status >= 500 {
			n++
		}
	}
	return n
}

func (p *Proxy) Spent() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.spent
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	run, rest, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/r/"), "/")
	if !ok || run == "" {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(rest, "models"):
		writeJSON(w, 200, map[string]any{"object": "list", "data": []map[string]any{{"id": p.Model, "object": "model"}}})
	case strings.HasSuffix(rest, "chat/completions") && r.Method == http.MethodPost:
		p.complete(w, r, run)
	default:
		http.NotFound(w, r)
	}
}

func (p *Proxy) complete(w http.ResponseWriter, r *http.Request, run string) {
	p.mu.Lock()
	over := p.Cap > 0 && p.spent >= p.Cap
	p.mu.Unlock()
	if over {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": "benchmark spend cap reached"}})
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"error": map[string]any{"message": "invalid JSON body"}})
		return
	}
	asked, _ := body["model"].(string)
	// Every system gets the same model and reasoning, whatever it asked for.
	body["model"], body["reasoning_effort"] = p.Model, p.Effort
	delete(body, "reasoning")
	delete(body, "thinking")
	stream, _ := body["stream"].(bool)
	if stream {
		options, _ := body["stream_options"].(map[string]any)
		if options == nil {
			options = map[string]any{}
		}
		options["include_usage"] = true
		body["stream_options"] = options
	}
	encoded, _ := json.Marshal(body)
	// A harness's first call can be a side request, such as a title, so record the first that offers tools.
	if _, tools := body["tools"]; p.First != nil && tools {
		p.mu.Lock()
		first := !p.recorded[run]
		if p.recorded == nil {
			p.recorded = map[string]bool{}
		}
		p.recorded[run] = true
		p.mu.Unlock()
		if first {
			p.First(run, encoded)
		}
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(p.Upstream, "/")+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	session := "bench-" + run
	if own := r.Header.Get("x-opencode-session"); own != "" {
		session += "-" + own
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.Key)
	request.Header.Set("User-Agent", "polymux-bench/0.1")
	request.Header.Set("x-opencode-session", session)

	p.mu.Lock()
	p.inflight[run]++
	p.mu.Unlock()
	call := Call{At: time.Now(), Asked: asked}
	defer func() {
		call.Latency = time.Since(call.At)
		p.mu.Lock()
		p.inflight[run]--
		p.calls[run] = append(p.calls[run], call)
		p.spent += call.Billed
		p.mu.Unlock()
	}()

	response, err := p.client.Do(request)
	if err != nil {
		call.Status = 502
		writeJSON(w, 502, map[string]any{"error": map[string]any{"message": "upstream: " + err.Error()}})
		return
	}
	defer response.Body.Close()
	call.Status = response.StatusCode
	w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	w.WriteHeader(response.StatusCode)
	if !stream || response.StatusCode != 200 {
		data, _ := io.ReadAll(response.Body)
		_, _ = w.Write(data)
		call.meter(data)
		// A spent allowance fails every later call, so the suite stops rather than score the provider's outage.
		if response.StatusCode == http.StatusTooManyRequests && bytes.Contains(data, []byte("UsageLimit")) {
			p.mu.Lock()
			p.exhausted = true
			p.mu.Unlock()
		}
		return
	}
	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReaderSize(response.Body, 64<<10)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			_, _ = w.Write(line)
			if flusher != nil {
				flusher.Flush()
			}
			if data, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data: ")); ok && bytes.Contains(data, []byte(`"usage"`)) {
				call.meter(data)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				call.Status = 499
			}
			return
		}
	}
}

// meter reads usage from a response or final stream chunk.
func (c *Call) meter(data []byte) {
	var payload struct {
		Usage *struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
			Details    struct {
				Cached int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			Hit      int `json:"prompt_cache_hit_tokens"`
			OpenCode struct {
				Cost *float64 `json:"list_cost"`
			} `json:"opencode"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Usage == nil {
		return
	}
	u := payload.Usage
	c.Prompt, c.Completion, c.Cached = u.Prompt, u.Completion, max(u.Details.Cached, u.Hit)
	c.Cost = (float64(c.Prompt-c.Cached)*priceInput + float64(c.Cached)*priceCached + float64(c.Completion)*priceOutput) / 1e6
	c.Billed = c.Cost
	if u.OpenCode.Cost != nil {
		c.Billed = *u.OpenCode.Cost
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
