package server

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/apps/server/internal/testenv"
	"github.com/MichaelKinsy/PiG/agent"
)

const testHTML = `<html><head><style>body{color:red}</style><title>Hidden</title></head><body><h1>Orange balloon</h1><button>Count &amp; compare</button><script>window.secret='hidden code'</script></body></html>`

type htmlFactory struct{}

func (*htmlFactory) Open(_ context.Context, d agentruntime.Definition, _ string) (agentruntime.Session, error) {
	for _, tool := range d.ExtraTools {
		if tool.Name() == "show_html" {
			return &htmlSession{tool: tool}, nil
		}
	}
	return nil, errors.New("show_html capability is missing")
}

type htmlSession struct{ tool agent.AgentTool }

func (s *htmlSession) Send(ctx context.Context, _ contract.Message, emit func(agentruntime.Delta) error) (agentruntime.Result, error) {
	if err := emit(agentruntime.Delta{Text: "Hello "}); err != nil {
		return agentruntime.Result{}, err
	}
	params, _ := json.Marshal(map[string]any{"html": testHTML, "title": "Balloon", "width": 320.5, "height": 180})
	if err := emit(agentruntime.Delta{Tool: "show_html", Args: string(params)}); err != nil {
		return agentruntime.Result{}, err
	}
	var group sync.WaitGroup
	problems := make(chan error, 3)
	for range 2 {
		group.Go(func() {
			result, err := s.tool.Execute(ctx, "card-call", params, nil)
			if err == nil {
				data, marshalErr := json.Marshal(result)
				if marshalErr != nil {
					err = marshalErr
				} else if strings.Contains(string(data), "<html>") || strings.Contains(string(data), "hidden code") {
					err = errors.New("tool receipt leaked raw HTML")
				}
			}
			problems <- err
		})
	}
	group.Go(func() { problems <- emit(agentruntime.Delta{Text: "there"}) })
	group.Wait()
	close(problems)
	for err := range problems {
		if err != nil {
			return agentruntime.Result{}, err
		}
	}
	return agentruntime.Result{Text: "Hello there"}, nil
}
func (*htmlSession) Abort(context.Context) error { return nil }
func (*htmlSession) Close() error                { return nil }
func (*htmlSession) Path() string                { return "html.jsonl" }

func TestHTMLReplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "polymux.db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(t.Context(), db, Config{DataDir: dir, Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}}}, Factory: &htmlFactory{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	web := httptest.NewServer(api)
	t.Cleanup(func() { web.Close(); api.Close(); db.Close() })
	for turn := range 2 {
		response, err := http.Post(web.URL+"/threads/assistant/messages", "application/json", strings.NewReader(fmt.Sprintf(`{"id":"u%d","text":"make a card","attachmentIDs":[]}`, turn)))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("send: %d", response.StatusCode)
		}
		deadline := time.Now().Add(3 * time.Second)
		settled := false
		for time.Now().Before(deadline) {
			messages, err := db.Messages(t.Context(), "assistant", 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) == (turn+1)*2 && messages[len(messages)-1].Status == "done" {
				settled = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !settled {
			t.Fatal("card reply did not settle")
		}
	}
	messages, err := db.Messages(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		if len(m.Parts) != 2 {
			t.Fatalf("duplicate text/card or raw tool projection: %+v", m.Parts)
		}
		var text string
		var card *contract.Attachment
		for _, p := range m.Parts {
			if p.Kind == "text" && p.Text != nil {
				text = *p.Text
			}
			if p.Kind == "attachment" {
				card = p.Attachment
			}
		}
		if text != "Hello there" || card == nil || card.Kind != "html" || card.MimeType != "text/html" || card.IndexingState != "ready" || card.ExtractedText == nil || *card.ExtractedText != "Orange balloon Count & compare" {
			t.Fatalf("card projection: %+v text=%q", card, text)
		}
		if card.Width == nil || *card.Width != 320.5 || card.Height == nil || *card.Height != 180 {
			t.Fatalf("card dimensions were not persisted: %+v", card)
		}
		ids = append(ids, card.ID)
		response, err := http.Get(web.URL + card.URL)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || string(data) != testHTML || response.Header.Get("Content-Type") != "text/html" {
			t.Fatalf("card download: %q %v", data, readErr)
		}
	}
	if ids[0] == ids[1] {
		t.Fatal("cached session reused the previous reply's card identity")
	}
	for _, m := range messages {
		if m.Role == "assistant" {
			_ = api.config.Index.Ingest(t.Context(), m)
		}
	}
	hits, _, err := api.config.Index.Search(t.Context(), "assistant", "orange balloon")
	if err != nil || len(hits) != 2 {
		t.Fatalf("card text is not searchable: %d %v", len(hits), err)
	}
	hits, _, err = api.config.Index.Search(t.Context(), "assistant", "hidden code")
	if err != nil || len(hits) != 0 {
		t.Fatalf("script source was indexed as card content: %d %v", len(hits), err)
	}
	events, err := db.Events(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	cards := 0
	for _, event := range events {
		if event.Type == "message.tool_call" {
			t.Fatal("HTML tool arguments were projected")
		}
		if event.Type == "message.attachment" {
			cards++
			if event.Message == nil || event.Message.Seq >= event.Seq {
				t.Fatalf("missing full stable message snapshot: %+v", event)
			}
			count := 0
			for _, part := range event.Message.Parts {
				if part.Attachment != nil {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("attachment replay has %d cards", count)
			}
		}
	}
	if cards != 2 {
		t.Fatalf("expected one attachment event per tool identity, got %d", cards)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, web.URL+"/threads/assistant/stream?after=0", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	for _, expected := range events {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var actual contract.Event
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &actual); err != nil {
				t.Fatal(err)
			}
			want, _ := json.Marshal(expected)
			got, _ := json.Marshal(actual)
			if string(want) != string(got) {
				t.Fatalf("SSE snapshot differs at seq %d", expected.Seq)
			}
			break
		}
	}
	response.Body.Close()
	web.Close()
	if err := api.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.Messages(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(messages)
	got, _ := json.Marshal(loaded)
	if string(want) != string(got) {
		t.Fatal("card transcript changed on reload")
	}
	for _, id := range ids {
		_, path, err := reopened.Attachment(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != testHTML {
			t.Fatalf("durable HTML content: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("card permissions: %v", err)
		}
	}
}

func TestHTMLValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		params string
	}{
		{"empty", `{"html":" "}`},
		{"missing", `{"title":"Card"}`},
		{"unknown", `{"html":"<p>x</p>","url":"https://example.com"}`},
		{"trailing", `{"html":"<p>x</p>"} {}`},
		{"oversize", fmt.Sprintf(`{"html":%q}`, strings.Repeat("x", htmlLimit+1))},
		{"width small", `{"html":"<p>x</p>","width":43.9}`},
		{"width large", `{"html":"<p>x</p>","width":550.1}`},
		{"height small", `{"html":"<p>x</p>","height":43.9}`},
		{"height large", `{"html":"<p>x</p>","height":400.1}`},
		{"width null", `{"html":"<p>x</p>","width":null}`},
		{"height null", `{"html":"<p>x</p>","height":null}`},
		{"width string", `{"html":"<p>x</p>","width":"320"}`},
		{"height string", `{"html":"<p>x</p>","height":"180"}`},
		{"width boolean", `{"html":"<p>x</p>","width":true}`},
		{"height array", `{"html":"<p>x</p>","height":[]}`},
		{"nonfinite", `{"html":"<p>x</p>","width":1e999}`},
		{"unknown component", `{"html":"<lg-toggle>x</lg-toggle>"}`},
		{"remote image", `{"html":"<img src=\"https://example.com/x.png\">"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			ctx := context.WithValue(t.Context(), htmlKey{}, &htmlScope{messageID: "m", publish: func(context.Context, contract.Attachment, string) error { called = true; return nil }})
			if _, err := (&htmlTool{}).Execute(ctx, "call", json.RawMessage(test.params), nil); err == nil || called {
				t.Fatalf("invalid card published: %v", err)
			}
		})
	}
}

func TestCardIssues(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		want   string
	}{
		{"button", `<lg-button>Save</lg-button>`, ""},
		{"controls", `<lg-switch checked>Notify</lg-switch><lg-slider value="40" label="Volume"></lg-slider>`, ""},
		{"segmented", `<lg-segmented value="day"><button value="day">Day</button><button value="week">Week</button></lg-segmented>`, ""},
		{"panel", `<lg-panel style="--lg-surface:transparent"><lg-button>Go</lg-button></lg-panel>`, ""},
		{"document", testHTML, ""},
		{"data image", `<img src="data:image/png;base64,AAAA">`, ""},
		{"fragment link", `<a href="#part">Details</a>`, ""},
		{"fragment filter", `<svg><filter id="f"></filter></svg><div style="filter:url(#f)"></div>`, ""},
		{"unknown component", `<lg-toggle>On</lg-toggle>`, "unknown <lg-toggle>"},
		{"page component", `<lg-navbar></lg-navbar>`, "<lg-navbar> is page-level"},
		{"remote image", `<img src="https://example.com/a.png">`, "<img src> must be a data: URL"},
		{"remote script", `<script src="https://example.com/a.js"></script>`, "<script src> is blocked"},
		{"data script", `<script src="data:text/javascript,x"></script>`, "<script src> is blocked"},
		{"iframe", `<iframe src="data:text/html,x"></iframe>`, "<iframe> is blocked"},
		{"remote link", `<a href="https://example.com">Docs</a>`, "<a href> cannot navigate"},
		{"form action", `<form action="/go"><button>Go</button></form>`, "<form action> cannot submit"},
		{"remote css", `<div style="background:url(https://example.com/a.png)"></div>`, "external url()"},
		{"import", `<style>@import url(data:text/css,x);</style>`, "@import is blocked"},
		{"duplicate id", `<span id="a"></span><span id="a"></span>`, `duplicate id "a"`},
		{"segmented text", `<lg-segmented><span>Day</span></lg-segmented>`, "<lg-segmented> options"},
		{"segmented short", `<lg-segmented><button value="day">Day</button></lg-segmented>`, "two or more"},
	} {
		t.Run(test.name, func(t *testing.T) {
			issues := strings.Join(cardIssues(test.source), "; ")
			if test.want == "" && issues != "" {
				t.Fatalf("unexpected issues: %s", issues)
			}
			if test.want != "" && !strings.Contains(issues, test.want) {
				t.Fatalf("issues %q do not mention %q", issues, test.want)
			}
		})
	}
}

func TestHTMLDimensions(t *testing.T) {
	for _, test := range []struct {
		name   string
		params string
		width  *float64
		height *float64
	}{
		{"measured", `{"html":"<p>x</p>"}`, nil, nil},
		{"minimum", `{"html":"<p>x</p>","width":44,"height":44}`, new(float64(44)), new(float64(44))},
		{"maximum", `{"html":"<p>x</p>","width":550,"height":400}`, new(float64(550)), new(float64(400))},
		{"fractional", `{"html":"<p>x</p>","width":212.5,"height":144.25}`, new(float64(212.5)), new(float64(144.25))},
		{"width only", `{"html":"<p>x</p>","width":200}`, new(float64(200)), nil},
		{"height only", `{"html":"<p>x</p>","height":100}`, nil, new(float64(100))},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var artifact contract.Attachment
			ctx := context.WithValue(t.Context(), htmlKey{}, &htmlScope{messageID: "m", publish: func(ctx context.Context, a contract.Attachment, path string) error {
				artifact = a
				return db.SaveAttachment(ctx, a, path)
			}})
			tool := &htmlTool{store: db, dir: dir}
			for range 2 {
				if _, err := tool.Execute(ctx, "call", json.RawMessage(test.params), nil); err != nil {
					t.Fatal(err)
				}
			}
			if !sameDimension(artifact.Width, test.width) || !sameDimension(artifact.Height, test.height) {
				t.Fatalf("dimensions: %+v", artifact)
			}
			for _, changed := range []string{`{"html":"<p>x</p>","width":201}`, `{"html":"<p>x</p>","height":101}`} {
				if _, err := tool.Execute(ctx, "call", json.RawMessage(changed), nil); err == nil {
					t.Fatal("tool identity accepted changed dimensions")
				}
			}
		})
	}
}

func TestHTMLRuntime(t *testing.T) {
	testenv.PiG(t)
	var turns atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		found := false
		for _, tool := range request.Tools {
			found = found || tool.Function.Name == "show_html"
		}
		if !found {
			t.Error("PiG did not advertise show_html")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(delta any, finish any) {
			data, _ := json.Marshal(map[string]any{"id": "test", "object": "chat.completion.chunk", "model": "test", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		if turns.Add(1) == 1 {
			params, _ := json.Marshal(map[string]string{"html": testHTML, "title": "Runtime card"})
			write(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "card_runtime", "type": "function", "function": map[string]any{"name": "show_html", "arguments": string(params)}}}}, nil)
			write(map[string]any{}, "tool_calls")
		} else {
			write(map[string]any{"role": "assistant", "content": "Shown."}, nil)
			write(map[string]any{}, "stop")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	dir := t.TempDir()
	runtime := durableRuntime(t, dir, provider.URL)
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api, err := New(t.Context(), db, Config{DataDir: dir, Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant", Model: "local/test"}}}, Factory: runtime, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	web := httptest.NewServer(api)
	defer web.Close()
	response, err := http.Post(web.URL+"/threads/assistant/messages", "application/json", strings.NewReader(`{"id":"runtime-user","text":"make a card","attachmentIDs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := db.Messages(t.Context(), "assistant", 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) == 2 && messages[1].Status != "streaming" {
			m := messages[1]
			if m.Status != "done" || len(m.Parts) != 2 || m.Parts[0].Attachment == nil || m.Parts[0].Attachment.Kind != "html" || m.Parts[1].Text == nil || *m.Parts[1].Text != "Shown." || turns.Load() != 2 {
				events, _ := db.Events(t.Context(), "assistant", 0, 100)
				t.Fatalf("native runtime tool flow: %+v events=%+v turns=%d", m, events, turns.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native runtime tool turn did not settle")
}

func TestHTMLCancel(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	attachments := filepath.Join(dir, "attachments")
	if err := os.Mkdir(attachments, 0700); err != nil {
		t.Fatal(err)
	}
	tool := &htmlTool{store: db, dir: attachments}
	params := json.RawMessage(`{"html":"<p>Card</p>","title":"../../Safe"}`)
	if _, err := tool.Execute(t.Context(), "call", params, nil); err == nil {
		t.Fatal("card escaped active reply")
	}
	ctx, cancel := context.WithCancel(t.Context())
	ctx = context.WithValue(ctx, htmlKey{}, &htmlScope{messageID: "m", publish: func(context.Context, contract.Attachment, string) error {
		t.Fatal("canceled tool published")
		return nil
	}})
	cancel()
	if _, err := tool.Execute(ctx, "call", params, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	ctx = context.WithValue(t.Context(), htmlKey{}, &htmlScope{messageID: "m", publish: func(ctx context.Context, a contract.Attachment, path string) error {
		if a.Name != "Safe.html" {
			t.Fatalf("unsafe filename: %q", a.Name)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		return context.Canceled
	}})
	if _, err := tool.Execute(ctx, "call", params, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("publication cancellation: %v", err)
	}
	files, err := os.ReadDir(attachments)
	if err != nil || len(files) != 0 {
		t.Fatalf("unpublished artifact survived: %v %v", files, err)
	}
	var count int
	if err := db.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM attachments`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unpublished attachment survived: %d %v", count, err)
	}
	if _, _, err := db.Attachment(t.Context(), "call"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
}
