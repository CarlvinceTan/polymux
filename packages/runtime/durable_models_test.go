package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable/storage"
)

// Isolates the models layer: does Polymux's OpenAI-compatible endpoint stream through PiG?
func TestDurableModelsStream(t *testing.T) {
	var requests int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Logf("request %d to %s", requests, r.URL.Path)
		sse(w, "READY", nil)
	}))
	defer provider.Close()
	models := durableModels(provider.URL+"/v1", "test-key", "test-model", Client{})
	model := models.GetModel(DurableProvider, "test-model")
	if model == nil {
		t.Fatal("model not registered")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream := models.StreamSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}})
	for event := range stream.Events(ctx) {
		t.Logf("event %s", event.EventType())
		if event.EventType() == ai.EventError {
			t.Fatalf("provider error: %+v", event)
		}
	}
	result := stream.Result()
	if result == nil {
		t.Fatal("no terminal result")
	}
	if requests == 0 {
		t.Fatal("no provider request was made")
	}
}

// TestPERetryAfter fails when a 429 is returned at once or the wait is not Retry-After.
// The test clock records the wait, so a real sleep is a stall.
func TestPERetryAfter(t *testing.T) {
	clock := &recordClock{}
	useClock(t, clock)
	var requests int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit"}}`))
			return
		}
		sse(w, "after retry", nil)
	}))
	defer provider.Close()
	models := durableModels(provider.URL+"/v1", "test-key", "test-model", Client{})
	model := models.GetModel(DurableProvider, "test-model")
	if model == nil {
		t.Fatal("model not registered")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	stream := models.StreamSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}})
	for event := range stream.Events(ctx) {
		if event.EventType() == ai.EventError {
			t.Fatalf("provider error: %+v", event)
		}
	}
	if time.Since(started) > time.Second {
		t.Fatal("retry slept instead of using the test clock")
	}
	if requests != 2 {
		t.Fatalf("requests=%d, want 2", requests)
	}
	if got := clock.durations(); len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("retry wait %v, want 2s", got)
	}
	result := stream.Result()
	if result == nil || result.StopReason == ai.StopReasonError {
		t.Fatalf("retry did not recover: %+v", result)
	}
}

// TestPENoRetryOnHardStatus fails when a 400 or 401 is retried, including one whose body says overloaded.
// The 429 probe fails when no retry policy is configured, so a build that never retries cannot pass.
func TestPENoRetryOnHardStatus(t *testing.T) {
	clock := &recordClock{}
	useClock(t, clock)
	attempts := func(status int, retryAfter, body string) (int, *ai.AssistantMessage) {
		t.Helper()
		var requests int
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if requests == 1 {
				w.Header().Set("Content-Type", "application/json")
				if retryAfter != "" {
					w.Header().Set("Retry-After", retryAfter)
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"` + body + `","type":"fixture_error"}}`))
				return
			}
			sse(w, "after retry", nil)
		}))
		defer provider.Close()
		models := durableModels(provider.URL+"/v1", "test-key", "test-model", Client{})
		model := models.GetModel(DurableProvider, "test-model")
		if model == nil {
			t.Fatal("model not registered")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		stream := models.StreamSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}})
		for range stream.Events(ctx) {
		}
		return requests, stream.Result()
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized} {
		requests, result := attempts(status, "30", "overloaded")
		if requests != 1 || result == nil || result.StopReason != ai.StopReasonError {
			t.Fatalf("%d retried or succeeded: requests=%d result=%+v", status, requests, result)
		}
	}
	if got := clock.durations(); len(got) != 0 {
		t.Fatalf("hard status waited %v", got)
	}
	requests, result := attempts(http.StatusTooManyRequests, "0", "overloaded")
	if requests != 2 || result == nil || result.StopReason == ai.StopReasonError {
		t.Fatalf("retry policy inactive: requests=%d result=%+v", requests, result)
	}
}
func TestDurableModelsRegistersEveryModel(t *testing.T) {
	models := durableModels("http://localhost/v1", "", "local/opencode", Client{}, "local/second", "local/cursor-agent", "local/second")
	for _, id := range []string{"local/opencode", "local/second", "local/cursor-agent"} {
		if models.GetModel(DurableProvider, id) == nil {
			t.Errorf("model %s not registered", id)
		}
	}
	if models.GetModel(DurableProvider, "missing") != nil {
		t.Fatal("unknown model registered")
	}
}

func TestDurableModelsSendsOwnModelID(t *testing.T) {
	requests := make(chan string, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Header.Get("X-Bridge") != "shared" {
			t.Error("missing shared header")
		}
		requests <- body.Model
		sse(w, "READY", nil)
	}))
	defer provider.Close()
	models := durableModels(provider.URL+"/v1", "test-key", "local/opencode", Client{Headers: map[string]string{"X-Bridge": "shared"}}, "local/second", "local/cursor-agent")
	for _, id := range []string{"local/opencode", "local/second", "local/cursor-agent"} {
		t.Run(id, func(t *testing.T) {
			model := models.GetModel(DurableProvider, id)
			if model == nil {
				t.Fatal("model not registered")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			stream := models.StreamSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}})
			for event := range stream.Events(ctx) {
				if event.EventType() == ai.EventError {
					t.Fatalf("provider error: %+v", event)
				}
			}
			select {
			case got := <-requests:
				if got != id {
					t.Fatalf("request model = %q, want %q", got, id)
				}
			default:
				t.Fatal("no request")
			}
		})
	}
}

func TestDurableAgentOnNonDefaultModelReplies(t *testing.T) {
	for _, id := range []string{"local/second", "local/cursor-agent"} {
		t.Run(id, func(t *testing.T) {
			requests := make(chan string, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				requests <- body.Model
				sse(w, "READY", nil)
			}))
			defer provider.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			// The runtime starts with only the default model; Open must make the agent's model resolvable.
			runtime, err := NewDurable(ctx, t.TempDir(), provider.URL+"/v1", "local/opencode", "test-key", Client{}, storage.NewMemoryStorage())
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			session, err := runtime.Open(ctx, Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Model: id}}, "")
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			result, err := session.Send(ctx, turn("hi"), nil)
			if err != nil || result.Text != "READY" {
				t.Fatalf("turn = %+v, error = %v", result, err)
			}
			select {
			case got := <-requests:
				if got != id {
					t.Fatalf("request model = %q, want %q", got, id)
				}
			default:
				t.Fatal("no request")
			}
		})
	}
}

type modelSecrets map[string]string

func (s modelSecrets) Get(id string) (string, error) { return s[id], nil }
func (s modelSecrets) Set(id, value string) error    { s[id] = value; return nil }
func (s modelSecrets) Delete(id string) error        { delete(s, id); return nil }

type modelTransport func(*http.Request) (*http.Response, error)

func (f modelTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBuiltinSubscriptionRouting(t *testing.T) {
	claim := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"test-account"}}`))
	for _, tc := range []struct {
		kind, access, path, header, value string
	}{
		{kind: "anthropic", access: "sk-ant-oat01-fresh", path: "/v1/messages", header: "Authorization", value: "Bearer sk-ant-oat01-fresh"},
		{kind: "openai-codex", access: "header." + claim + ".signature", path: "/backend-api/codex/responses", header: "chatgpt-account-id", value: "test-account"},
		{kind: "openai", access: "direct-token", path: "/v1/responses", header: "Authorization", value: "Bearer direct-token"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			builtin := *builtinProvider(tc.kind)
			oauth := *builtin.Auth.OAuth
			builtin.Auth.OAuth = &oauth
			refreshes := 0
			oauth.Refresh = func(_ context.Context, credential ai.Credential) (ai.Credential, error) {
				refreshes++
				credential.Access, credential.Expires = tc.access, time.Now().Add(time.Hour).UnixMilli()
				return credential, nil
			}
			credentials := &testCredentials{Secrets: modelSecrets{}}
			if _, err := credentials.Modify(t.Context(), SettingsProvider("work"), func(*ai.Credential) (*ai.Credential, error) {
				return &ai.Credential{Type: ai.CredentialOAuth, Access: "expired", Refresh: "refresh", Expires: 1}, nil
			}); err != nil {
				t.Fatal(err)
			}
			all, err := builtin.GetModels()
			if err != nil || len(all) == 0 {
				t.Fatal("no built-in models")
			}
			registry := newBuiltinRegistry(ProviderSpec{ID: "work", Builtin: &builtin, Credentials: credentials, Models: []string{all[0].ID}, Headers: map[string]string{"X-Polymux-Probe": "yes"}})
			models := registry.runtime()
			defer models.Close()
			requests := make(chan *http.Request, 2)
			fetch := &http.Client{Transport: modelTransport(func(r *http.Request) (*http.Response, error) {
				requests <- r.Clone(t.Context())
				return &http.Response{StatusCode: 401, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"fixture rejection"}}`))}, nil
			})}
			model := models.GetModel(SettingsProvider("work"), all[0].ID)
			for range 2 {
				stream := models.StreamSimple(t.Context(), model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}}, ai.StreamOptions{Fetch: fetch})
				for range stream.Events(t.Context()) {
				}
				select {
				case request := <-requests:
					if request.URL.Path != tc.path || request.Header.Get(tc.header) != tc.value || request.Header.Get("X-Polymux-Probe") != "yes" {
						t.Fatalf("wrong subscription request: %s header=%q", request.URL.Path, request.Header.Get(tc.header))
					}
					if tc.kind == "anthropic" && !strings.Contains(request.Header.Get("anthropic-beta"), "oauth-") {
						t.Fatal("missing Anthropic subscription headers")
					}
				default:
					t.Fatalf("no request: %+v", stream.Result())
				}
			}
			if refreshes != 1 {
				t.Fatalf("refreshed %d times, want once", refreshes)
			}
		})
	}
}

func TestDurableAgentRunsOnItsSettingsProvider(t *testing.T) {
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the fallback endpoint received a request meant for the Settings provider")
		sse(w, "WRONG", nil)
	}))
	defer fallback.Close()
	requests := make(chan map[string]any, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer settings-key" {
			t.Errorf("authorization = %q", got)
		}
		requests <- body
		sse(w, "READY", nil)
	}))
	defer provider.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	runtime, err := NewDurable(ctx, t.TempDir(), fallback.URL+"/v1", "fallback-model", "fallback-key", Client{}, storage.NewMemoryStorage())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	agent := Agent{ID: "assistant", Name: "Assistant", Model: "chosen", Provider: "work", Reasoning: "high"}
	if _, err := runtime.Open(ctx, Definition{Agent: agent}, ""); err == nil {
		t.Fatal("opened a bot on a provider that isn't set up")
	}
	runtime.SetProviders([]ProviderSpec{{ID: "work", Type: "openai", BaseURL: provider.URL + "/v1", Key: "settings-key", Models: []string{"chosen"}}})
	session, err := runtime.Open(ctx, Definition{Agent: agent}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.Send(ctx, turn("hi"), nil)
	if err != nil || result.Text != "READY" {
		t.Fatalf("turn = %+v, error = %v", result, err)
	}
	body := <-requests
	if body["model"] != "chosen" || body["reasoning_effort"] != "high" {
		t.Fatalf("request model = %v, reasoning = %v, body keys = %v", body["model"], body["reasoning_effort"], slices.Sorted(maps.Keys(body)))
	}
}
