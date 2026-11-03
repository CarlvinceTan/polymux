package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

func TestFallbackPriorityRecovery(t *testing.T) {
	runtime := openDurable(t, &httptest.Server{URL: "http://localhost"})
	var elapsed atomic.Int64
	now := time.Now()
	runtime.fallback.now = func() time.Time { return now.Add(time.Duration(elapsed.Load()) * time.Second) }
	var ready [3]atomic.Bool
	ready[2].Store(true)
	var mu sync.Mutex
	var calls []int
	var specs []ProviderSpec
	for i := range 3 {
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			calls = append(calls, i)
			mu.Unlock()
			if r.Header.Get("Authorization") != fmt.Sprintf("Bearer key-%d", i) {
				t.Error("connection used another connection's credentials")
			}
			if !ready[i].Load() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(429)
				fmt.Fprintf(w, `{"error":{"type":"insufficient_quota","resets_in_seconds":%d}}`, (i+1)*10)
				return
			}
			sse(w, fmt.Sprintf("provider-%d", i), nil)
		}))
		t.Cleanup(provider.Close)
		specs = append(specs, ProviderSpec{ID: fmt.Sprint(i), Type: "custom", BaseURL: provider.URL + "/v1", Key: fmt.Sprintf("key-%d", i), Models: []string{"chosen"}})
	}
	runtime.SetProviders(specs)
	// The bot chose the middle provider, so it goes before the user's first.
	model := runtime.catalogue.GetModel(SettingsProvider("1"), "chosen")
	check := func(want string, order ...int) {
		t.Helper()
		mu.Lock()
		calls = nil
		mu.Unlock()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		result := runtime.fallback.CompleteSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hello")}}})
		if result == nil || assistantText(result) != want {
			t.Fatalf("result=%+v, want %q", result, want)
		}
		mu.Lock()
		defer mu.Unlock()
		if !slices.Equal(calls, order) {
			t.Fatalf("providers tried %v, want %v", calls, order)
		}
	}
	check("provider-2", 1, 0, 2)
	check("provider-2", 2)
	ready[1].Store(true)
	elapsed.Store(21)
	check("provider-1", 1)
	ready[1].Store(false)
	runtime.SetProviders([]ProviderSpec{specs[2], specs[0], specs[1]})
	check("provider-2", 1, 2)
}

func TestFallbackAllUnavailable(t *testing.T) {
	runtime := openDurable(t, &httptest.Server{URL: "http://localhost"})
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid key"}}`)
	}))
	t.Cleanup(provider.Close)
	runtime.SetProviders([]ProviderSpec{
		{ID: "first", BaseURL: provider.URL, Models: []string{"chosen"}},
		{ID: "second", BaseURL: provider.URL, Models: []string{"chosen"}},
	})
	model := runtime.catalogue.GetModel(SettingsProvider("first"), "chosen")
	for range 2 {
		result := runtime.fallback.CompleteSimple(t.Context(), model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hello")}}})
		if result.StopReason != ai.StopReasonError || result.ErrorMessage != unavailableMessage {
			t.Fatalf("unexpected terminal error: %+v", result)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("failed connections were hammered during cooldown: %d calls", calls.Load())
	}
}

func TestFallbackContinuesTool(t *testing.T) {
	tool := &onceTool{}
	var primary atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if primary.Add(1) == 1 {
			sse(w, "", []map[string]any{{"index": 0, "id": "call-1", "type": "function", "function": map[string]string{"name": "once_tool", "arguments": "{}"}}})
			return
		}
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"type":"insufficient_quota"}}`)
	}))
	t.Cleanup(provider.Close)
	var backup atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backup.Add(1)
		var model string
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(data, &fields) != nil || json.Unmarshal(fields["model"], &model) != nil || model != "backup-model" {
			t.Errorf("wrong fallback model: %s", data)
		}
		if !strings.Contains(string(data), `"role":"tool"`) || !strings.Contains(string(data), `"content":"once"`) {
			t.Error("fallback lost the completed tool result")
		}
		sse(w, "FINISHED", nil)
	}))
	t.Cleanup(other.Close)
	runtime := openDurable(t, provider)
	runtime.SetProviders([]ProviderSpec{
		{ID: "primary", BaseURL: provider.URL + "/v1", Models: []string{"chosen"}},
		{ID: "backup", BaseURL: other.URL + "/v1", Models: []string{"backup-model"}},
	})
	session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Model: "chosen", Provider: "primary"}, Intro: true, ExtraTools: []agent.AgentTool{tool}}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := session.Send(ctx, turn("call once"), nil)
	if err != nil || result.Text != "FINISHED" {
		t.Fatalf("fallback failed: %+v, %v", result, err)
	}
	if primary.Load() != 2 || backup.Load() != 1 || tool.n.Load() != 1 {
		t.Fatalf("primary=%d backup=%d tool=%d", primary.Load(), backup.Load(), tool.n.Load())
	}
}

func TestFallbackNoReplay(t *testing.T) {
	var backups atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { dropAfterText(w, "ONCE") }))
	t.Cleanup(primary.Close)
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backups.Add(1)
		sse(w, "WRONG", nil)
	}))
	t.Cleanup(backup.Close)
	runtime := openDurable(t, primary)
	runtime.SetProviders([]ProviderSpec{
		{ID: "primary", BaseURL: primary.URL, Models: []string{"chosen"}},
		{ID: "backup", BaseURL: backup.URL, Models: []string{"chosen"}},
	})
	model := runtime.catalogue.GetModel(SettingsProvider("primary"), "chosen")
	result := runtime.fallback.CompleteSimple(t.Context(), model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hello")}}})
	if result.StopReason != ai.StopReasonError || backups.Load() != 0 || assistantText(result) != "ONCE" {
		t.Fatalf("partial output was replayed: result=%+v backups=%d", result, backups.Load())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result = runtime.fallback.CompleteSimple(ctx, model, ai.Context{})
	if result.StopReason != ai.StopReasonAborted || backups.Load() != 0 {
		t.Fatal("cancellation started fallback")
	}
}

func TestFallbackSetupTimeout(t *testing.T) {
	release := make(chan struct{})
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release); primary.Close() })
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sse(w, "READY", nil) }))
	t.Cleanup(backup.Close)
	runtime := openDurable(t, primary)
	runtime.fallback.timeout = 50 * time.Millisecond
	runtime.SetProviders([]ProviderSpec{
		{ID: "primary", BaseURL: primary.URL, Models: []string{"chosen"}},
		{ID: "backup", BaseURL: backup.URL, Models: []string{"chosen"}},
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := runtime.fallback.CompleteSimple(ctx, runtime.catalogue.GetModel(SettingsProvider("primary"), "chosen"), ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hello")}}})
	if assistantText(result) != "READY" {
		t.Fatalf("stalled setup did not fall back: %+v", result)
	}
}

func TestFallbackBuiltinAccounts(t *testing.T) {
	runtime := openDurable(t, &httptest.Server{URL: "http://localhost"})
	credentials := &testCredentials{Secrets: modelSecrets{"first": "first-key", "second": "second-key"}}
	builtin := *builtinProvider("openai")
	builtin.GetModels = func() ([]*ai.Model, error) {
		return []*ai.Model{{ID: "chosen", ProviderMeta: ai.ProviderMetadata{ProviderID: "openai", API: ai.APIOpenAICompletions}, Capabilities: ai.ModelCapabilities{ContextWindow: DefaultWindow}}}, nil
	}
	var keys []string
	builtin.StreamSimple = func(_ context.Context, model *ai.Model, _ ai.TranscriptContext, opts ai.StreamOptions) (*ai.AssistantMessageEventStream, error) {
		keys = append(keys, opts.APIKey)
		if header := opts.Headers["X-Account"]; header == nil || *header != strings.TrimSuffix(opts.APIKey, "-key") {
			t.Error("fallback reused another account's headers")
		}
		stream := ai.NewAssistantMessageEventStream()
		result := &ai.AssistantMessage{Provider: model.ProviderID(), Model: model.ID, Content: []ai.AssistantContentBlock{}, StopReason: ai.StopReasonError, ErrorMessage: "quota exceeded"}
		if opts.APIKey == "second-key" {
			result.StopReason, result.ErrorMessage = ai.StopReasonStop, ""
			result.Content = []ai.AssistantContentBlock{ai.TextContent{Text: "READY"}}
		}
		stream.End(result)
		return stream, nil
	}
	runtime.SetProviders([]ProviderSpec{
		{ID: "first", Builtin: &builtin, Credentials: credentials, Models: []string{"chosen"}, Headers: map[string]string{"X-Account": "first"}},
		{ID: "second", Builtin: &builtin, Credentials: credentials, Models: []string{"chosen"}, Headers: map[string]string{"X-Account": "second"}},
	})
	result := runtime.fallback.CompleteSimple(t.Context(), runtime.catalogue.GetModel(SettingsProvider("first"), "chosen"), ai.Context{})
	if assistantText(result) != "READY" || strings.Join(keys, ",") != "first-key,second-key" {
		t.Fatalf("independent account routing failed: keys=%v result=%+v", keys, result)
	}
}

func TestFallbackRemovedConnection(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sse(w, "READY", nil) }))
	t.Cleanup(provider.Close)
	runtime := openDurable(t, provider)
	first := ProviderSpec{ID: "first", BaseURL: provider.URL, Models: []string{"chosen"}}
	second := ProviderSpec{ID: "second", BaseURL: provider.URL, Models: []string{"chosen"}}
	runtime.SetProviders([]ProviderSpec{first, second})
	definition := Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Provider: "first", Model: "chosen"}, Intro: true}
	session, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	runtime.SetProviders([]ProviderSpec{second})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := session.Send(ctx, turn("hello"), nil)
	if !errors.Is(err, ErrProviderMissing) {
		t.Fatalf("removing a provider silently remapped its chat: %+v %v", result, err)
	}
	if _, err := runtime.Open(ctx, definition, ""); !errors.Is(err, ErrProviderMissing) {
		t.Fatal("opening a bot silently remapped its removed provider:", err)
	}
	definition.Agent.Provider = "second"
	other, err := runtime.Open(ctx, definition, "")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := other.Send(ctx, turn("hello"), nil); err != nil || result.Text != "READY" {
		t.Fatalf("explicit remap failed: %+v %v", result, err)
	}
	_ = other.Close()
}

func TestFallbackSkipsModels(t *testing.T) {
	for _, name := range []string{"not enabled", "context too small"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				sse(w, "WRONG", nil)
			}))
			t.Cleanup(first.Close)
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sse(w, "READY", nil) }))
			t.Cleanup(second.Close)
			runtime := openDurable(t, first)
			spec := ProviderSpec{ID: "first", BaseURL: first.URL, Windows: map[string]int{"chosen": 1024}}
			if name == "context too small" {
				spec.Models = []string{"chosen"}
			}
			runtime.SetProviders([]ProviderSpec{spec, {ID: "second", BaseURL: second.URL, Models: []string{"chosen"}}})
			session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Provider: "first", Model: "chosen"}, Intro: true}, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result := runtime.fallback.CompleteSimple(ctx, runtime.catalogue.GetModel(SettingsProvider("first"), "chosen"), ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hello")}}})
			if assistantText(result) != "READY" || calls.Load() != 0 {
				t.Fatalf("ineligible model was used: result=%+v calls=%d", result, calls.Load())
			}
		})
	}
}

func TestProviderReset(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, test := range []struct {
		name   string
		header http.Header
		body   string
		want   time.Duration
	}{
		{name: "retry after", header: http.Header{"Retry-After": {"120"}}, want: 2 * time.Minute},
		{name: "reset seconds", body: `{"error":{"resets_in_seconds":3600}}`, want: time.Hour},
		{name: "reset timestamp", body: `{"error":{"resets_at":1800000060}}`, want: time.Minute},
		{name: "header duration", header: http.Header{"X-Ratelimit-Reset-Requests": {"2m10s"}}, want: 130 * time.Second},
		{name: "past reset", body: `{"reset_at":1799999999}`, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := &providerFailure{now: now}
			failure.capture(test.header, []byte(test.body))
			if failure.delay != test.want {
				t.Fatalf("delay=%v, want %v", failure.delay, test.want)
			}
		})
	}
}

func TestFallbackRejectedRequest(t *testing.T) {
	runtime := openDurable(t, &httptest.Server{URL: "http://localhost"})
	var rejecting atomic.Bool
	rejecting.Store(true)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rejecting.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"message":"This model does not support images"}}`)
			return
		}
		sse(w, "accepted", nil)
	}))
	t.Cleanup(provider.Close)
	runtime.SetProviders([]ProviderSpec{{ID: "only", BaseURL: provider.URL, Models: []string{"chosen"}}})
	model := runtime.catalogue.GetModel(SettingsProvider("only"), "chosen")
	request := ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hello")}}}
	// The provider's reason reaches the user, and one bad request doesn't block the next.
	result := runtime.fallback.CompleteSimple(t.Context(), model, request)
	if result.StopReason != ai.StopReasonError || !strings.Contains(result.ErrorMessage, "does not support images") {
		t.Fatalf("rejection = %+v", result)
	}
	rejecting.Store(false)
	if result := runtime.fallback.CompleteSimple(t.Context(), model, request); assistantText(result) != "accepted" {
		t.Fatalf("valid request after a rejection = %+v", result)
	}
}

func TestFallbackPolicy(t *testing.T) {
	type call struct{ provider, model, effort string }
	own := call{"own", "chosen", "high"}
	cases := []struct {
		name     string
		fallback Fallback
		down     []string
		reply    string
		tried    []call
	}{
		{name: "off stops on the bot's model", fallback: Fallback{Mode: FallbackOff}, tried: []call{own}},
		{name: "auto prefers the same model elsewhere", reply: "twin chosen", tried: []call{own, {"twin", "chosen", "high"}}},
		{name: "auto chooses the provider's preferred model", down: []string{"twin chosen"}, reply: "plain favourite",
			tried: []call{own, {"twin", "chosen", "high"}, {"plain", "favourite", "high"}}},
		{name: "custom follows its order and reasoning", fallback: Fallback{Mode: FallbackCustom, Models: []FallbackModel{{Provider: "plain", Model: "smaller", Reasoning: "low"}}},
			reply: "plain smaller", tried: []call{own, {"plain", "smaller", "low"}}},
		{name: "custom can stay on the provider", fallback: Fallback{Mode: FallbackCustom, Models: []FallbackModel{{Provider: "own", Model: "other", Reasoning: "max"}}},
			reply: "own other", tried: []call{own, {"own", "other", "high"}}},
		{name: "custom skips models it can't reach", fallback: Fallback{Mode: FallbackCustom, Models: []FallbackModel{{Provider: "plain", Model: "gone"}, {Provider: "twin", Model: "chosen", Reasoning: "medium"}}},
			reply: "twin chosen", tried: []call{own, {"twin", "chosen", "medium"}}},
		{name: "custom stops after its last model", fallback: Fallback{Mode: FallbackCustom, Models: []FallbackModel{{Provider: "twin", Model: "chosen", Reasoning: "low"}}},
			down: []string{"twin chosen"}, tried: []call{own, {"twin", "chosen", "low"}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			down := append([]string{"own chosen"}, test.down...)
			var mu sync.Mutex
			var tried []call
			endpoint := func(name string) *httptest.Server {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body struct {
						Model  string `json:"model"`
						Effort string `json:"reasoning_effort"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					mu.Lock()
					tried = append(tried, call{name, body.Model, body.Effort})
					mu.Unlock()
					if slices.Contains(down, name+" "+body.Model) {
						w.WriteHeader(429)
						_, _ = io.WriteString(w, `{"error":{"type":"insufficient_quota"}}`)
						return
					}
					sse(w, name+" "+body.Model, nil)
				}))
				t.Cleanup(server.Close)
				return server
			}
			first := endpoint("own")
			runtime := openDurable(t, first)
			runtime.SetProviders([]ProviderSpec{
				{ID: "own", BaseURL: first.URL + "/v1", Models: []string{"chosen", "other"}},
				{ID: "plain", BaseURL: endpoint("plain").URL + "/v1", Models: []string{"smaller", "favourite"}, Preferred: "favourite"},
				{ID: "twin", BaseURL: endpoint("twin").URL + "/v1", Models: []string{"chosen"}},
			})
			session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Model: "chosen", Provider: "own", Reasoning: "high", Fallback: test.fallback}, Intro: true}, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			// With every model spent, the turn waits for one to come back, so a short deadline ends it.
			limit := 10 * time.Second
			if test.reply == "" {
				limit = time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), limit)
			defer cancel()
			result, err := session.Send(ctx, turn("hello"), nil)
			if test.reply == "" && err == nil || test.reply != "" && (err != nil || result.Text != test.reply) {
				t.Fatalf("result=%+v err=%v, want %q", result, err, test.reply)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(tried, test.tried) {
				t.Fatalf("tried %v, want %v", tried, test.tried)
			}
		})
	}
}

func TestFallbackNotice(t *testing.T) {
	useClock(t, &recordClock{})
	var limited atomic.Bool
	limited.Store(true)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model == "chosen" {
			w.WriteHeader(429)
			if limited.Load() {
				_, _ = io.WriteString(w, `{"error":{"type":"insufficient_quota"}}`)
			}
			return
		}
		sse(w, body.Model, nil)
	}))
	t.Cleanup(provider.Close)
	runtime := openDurable(t, provider)
	runtime.SetProviders([]ProviderSpec{
		{ID: "own", BaseURL: provider.URL + "/v1", Models: []string{"chosen"}},
		{ID: "spare", BaseURL: provider.URL + "/v1", Models: []string{"backup"}},
	})
	session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Model: "chosen", Provider: "own"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	send := func(want string) {
		t.Helper()
		var notices []string
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		result, err := session.Send(ctx, turn("hello"), func(delta Delta) error {
			if delta.Notice != "" {
				notices = append(notices, delta.Notice)
			}
			return nil
		})
		if err != nil || result.Text != "backup" || !slices.Equal(notices, []string{want}) {
			t.Fatalf("result=%+v err=%v notices=%q, want %q", result, err, notices, want)
		}
	}
	// The second turn skips the cooling model but still says why.
	send("chosen hit its usage limit, so backup replied instead.")
	send("chosen hit its usage limit, so backup replied instead.")
	limited.Store(false)
	runtime.SetProviders([]ProviderSpec{
		{ID: "own", BaseURL: provider.URL + "/v1", Models: []string{"chosen"}},
		{ID: "spare", BaseURL: provider.URL + "/v1", Models: []string{"backup"}},
	})
	send("chosen is unavailable, so backup replied instead.")
}

func TestFallbackAttribution(t *testing.T) {
	var primary, backup atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primary.Add(1)
		w.WriteHeader(401)
	}))
	t.Cleanup(first.Close)
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backup.Add(1)
		sse(w, "BACKUP", nil)
	}))
	t.Cleanup(second.Close)
	runtime := openDurable(t, first)
	runtime.SetProviders([]ProviderSpec{{ID: "first", BaseURL: first.URL, Models: []string{"chosen"}}, {ID: "second", BaseURL: second.URL, Models: []string{"chosen"}}})
	model := runtime.catalogue.GetModel(SettingsProvider("first"), "chosen")
	route := runtime.fallback.bind("7", Fallback{Mode: FallbackOff})
	ask := func(text, session string) *ai.AssistantMessage {
		return runtime.fallback.CompleteSimple(t.Context(), model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText(text)}}}, ai.StreamOptions{SessionID: session})
	}
	// Repeats reach the model despite its cooldown, so a stopped bot can try again.
	for range 2 {
		if result := ask("hello\n"+conversationMarker("7"), "known"); result.ErrorMessage != modelUnavailable {
			t.Fatalf("off bot: %+v", result)
		}
	}
	if result := ask("summary", "known"); result.ErrorMessage != modelUnavailable {
		t.Fatalf("summary left an off bot's model: %+v", result)
	}
	if result := ask("summary", "unknown"); result.ErrorMessage != modelUnavailable {
		t.Fatalf("unattributed request left its model: %+v", result)
	}
	if primary.Load() != 4 || backup.Load() != 0 {
		t.Fatalf("primary=%d backup=%d", primary.Load(), backup.Load())
	}
	runtime.fallback.unbind("7", route)
	if assistantText(ask("no session", "")) != "BACKUP" {
		t.Fatal("a request with no session should follow auto")
	}
}
