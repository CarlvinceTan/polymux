package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/providers"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	"github.com/MichaelKinsy/PiG/ai"
)

func loginServer(t *testing.T) (*Server, *providers.Registry) {
	t.Helper()
	dir := t.TempDir()
	prefs, err := settings.Open(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	s := &Server{mux: http.NewServeMux(), ctx: ctx, config: Config{DataDir: dir}}
	reg := &providers.Registry{Settings: prefs, Secrets: providerSecrets{}}
	mu := &sync.Mutex{}
	s.providerRoutes(reg, mu)
	s.loginRoutes(reg, mu)
	return s, reg
}

func loginCall(s *Server, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

func waitLogin(t *testing.T, s *Server, state contract.ProviderLogin) contract.ProviderLogin {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for !state.Done && state.Prompt == nil && ctx.Err() == nil {
		path := "/providers/logins/" + state.ID + "?after=" + strconv.Itoa(state.Revision)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", path, nil).WithContext(ctx))
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil {
			t.Fatalf("poll: %d %s", w.Code, w.Body.String())
		}
	}
	if ctx.Err() != nil {
		t.Fatal("login did not advance")
	}
	return state
}

func startLogin(t *testing.T, s *Server, body string) contract.ProviderLogin {
	t.Helper()
	w := loginCall(s, "POST", "/providers/logins", body)
	var state contract.ProviderLogin
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"events":null`) {
		t.Fatal("events must always be a JSON array")
	}
	return waitLogin(t, s, state)
}

func answerLogin(t *testing.T, s *Server, state contract.ProviderLogin, value string) contract.ProviderLogin {
	t.Helper()
	body, err := json.Marshal(map[string]any{"prompt": state.Prompt.ID, "value": value})
	if err != nil {
		t.Fatal(err)
	}
	w := loginCall(s, "POST", "/providers/logins/"+state.ID+"/answer", string(body))
	if w.Code != 204 {
		t.Fatalf("answer: %d %s", w.Code, w.Body.String())
	}
	state.Prompt = nil
	return waitLogin(t, s, state)
}

func TestProviderLoginKey(t *testing.T) {
	t.Parallel()
	s, reg := loginServer(t)
	state := startLogin(t, s, `{"type":"openai","auth":"api_key"}`)
	if state.Prompt == nil || state.Prompt.Type != "secret" {
		t.Fatalf("expected PiG secret prompt: %+v", state)
	}
	prompt := state.Prompt.ID
	state = answerLogin(t, s, state, "private-login-key")
	if !state.Done || state.Error != "" || state.Provider == nil || state.Provider.Status != "api_key" || len(state.Provider.Models) == 0 {
		t.Fatalf("login did not publish provider/models: %+v", state)
	}
	id := state.Provider.ID
	if state.Provider.Name != "API key 1" || state.Provider.KeyHint != "priv••••-key" {
		t.Fatalf("key metadata missing: name=%q hint=%q", state.Provider.Name, state.Provider.KeyHint)
	}
	blocked := loginCall(s, "POST", "/providers/logins", `{"provider":"`+id+`","auth":"api_key"}`)
	if blocked.Code != http.StatusConflict {
		t.Fatal("saved API key accepted another sign-in")
	}
	credential, err := reg.Credentials().Read(t.Context(), providers.Prefix+id)
	if err != nil || credential == nil || credential.Key != "private-login-key" {
		t.Fatal("key not stored under runtime provider ID")
	}
	for _, path := range []string{"/providers", "/providers/logins/" + state.ID} {
		w := loginCall(s, "GET", path, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "private-login-key") {
			t.Fatal("login or provider response leaked a secret")
		}
	}
	w := loginCall(s, "POST", "/providers/logins/"+state.ID+"/answer", `{"prompt":`+strconv.Itoa(prompt)+`,"value":"duplicate"}`)
	if w.Code != 409 {
		t.Fatal("an old prompt accepted another answer")
	}
	w = loginCall(s, "DELETE", "/providers/"+id, "")
	if w.Code != 204 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if credential, err := reg.Credentials().Read(t.Context(), providers.Prefix+id); err != nil || credential != nil {
		t.Fatal("provider deletion retained credentials")
	}
}

func TestProviderLoginMultipleConnections(t *testing.T) {
	t.Parallel()
	s, reg := loginServer(t)
	var ids []string
	for i, key := range []string{"first-private-key", "second-private-key"} {
		state := startLogin(t, s, `{"type":"openai","auth":"api_key"}`)
		state = answerLogin(t, s, state, key)
		if state.Provider == nil || state.Error != "" {
			t.Fatalf("connection %d failed: %+v", i, state)
		}
		ids = append(ids, state.Provider.ID)
		if state.Provider.Name != []string{"API key 1", "API key 2"}[i] || state.Provider.Order != i {
			t.Fatalf("connection name/order=%+v", state.Provider)
		}
	}
	if ids[0] == ids[1] || len(reg.Settings.Providers()) != 2 {
		t.Fatal("connecting a second account replaced the first")
	}
	for i, key := range []string{"first-private-key", "second-private-key"} {
		credential, err := reg.Credentials().Read(t.Context(), providers.Prefix+ids[i])
		if err != nil || credential == nil || credential.Key != key {
			t.Fatal("accounts did not retain independent credentials")
		}
	}
	if w := loginCall(s, "DELETE", "/providers/"+ids[0], ""); w.Code != 204 {
		t.Fatal("couldn't remove the first connection")
	}
	credential, err := reg.Credentials().Read(t.Context(), providers.Prefix+ids[1])
	if err != nil || credential == nil || credential.Key != "second-private-key" {
		t.Fatal("removing one connection removed the other's credentials")
	}
}

func TestProviderLoginInheritsSettings(t *testing.T) {
	t.Parallel()
	s, _ := loginServer(t)
	first := startLogin(t, s, `{"type":"openai","auth":"api_key"}`)
	first = answerLogin(t, s, first, "first-inherited-key")
	if first.Provider == nil || len(first.Provider.Models) == 0 {
		t.Fatal("first key has no models")
	}
	id := first.Provider.Models[0].ID
	body, err := json.Marshal(map[string]any{"enabled": false, "models": map[string]bool{id: false}})
	if err != nil {
		t.Fatal(err)
	}
	if w := loginCall(s, "PATCH", "/providers/groups/openai", string(body)); w.Code != 200 {
		t.Fatalf("provider settings: %d %s", w.Code, w.Body.String())
	}
	second := startLogin(t, s, `{"type":"openai","auth":"api_key"}`)
	second = answerLogin(t, s, second, "second-inherited-key")
	if second.Provider == nil || second.Provider.Enabled || !slices.ContainsFunc(second.Provider.Models, func(model contract.ProviderModel) bool {
		return model.ID == id && !model.Enabled
	}) {
		t.Fatal("new key did not inherit provider settings")
	}
}

func TestProviderLoginMultipleFields(t *testing.T) {
	t.Parallel()
	s, reg := loginServer(t)
	state := startLogin(t, s, `{"type":"cloudflare-ai-gateway","auth":"api_key"}`)
	for _, value := range []string{"private-cloudflare-key", "account", "gateway"} {
		if state.Prompt == nil {
			t.Fatalf("missing next prompt: %+v", state)
		}
		state = answerLogin(t, s, state, value)
	}
	if !state.Done || state.Error != "" || state.Provider == nil {
		t.Fatalf("multi-field login failed: %+v", state)
	}
	credential, err := reg.Credentials().Read(t.Context(), providers.Prefix+state.Provider.ID)
	if err != nil || credential == nil || credential.Env["CLOUDFLARE_ACCOUNT_ID"] != "account" || credential.Env["CLOUDFLARE_GATEWAY_ID"] != "gateway" {
		t.Fatalf("provider configuration lost: %+v %v", credential, err)
	}
}

func TestProviderLoginSelectAndCancel(t *testing.T) {
	t.Parallel()
	s, reg := loginServer(t)
	state := startLogin(t, s, `{"type":"amazon-bedrock","auth":"api_key"}`)
	if state.Prompt == nil || state.Prompt.Type != "select" || len(state.Prompt.Options) == 0 {
		t.Fatalf("missing PiG select prompt: %+v", state)
	}
	state = answerLogin(t, s, state, "bearer-token")
	w := loginCall(s, "DELETE", "/providers/logins/"+state.ID, "")
	if w.Code != 204 {
		t.Fatal("cancel failed")
	}
	for !state.Done {
		state.Prompt = nil
		state = waitLogin(t, s, state)
	}
	if state.Error != "Sign-in cancelled." || len(reg.Settings.Providers()) != 0 {
		t.Fatalf("cancel published a provider: %+v", state)
	}
}

func TestProviderLoginRemovedWhileSigningIn(t *testing.T) {
	t.Parallel()
	s, reg := loginServer(t)
	if err := reg.Save([]providers.Provider{{ID: "existing", Type: "openai", Name: "Existing"}}); err != nil {
		t.Fatal(err)
	}
	state := startLogin(t, s, `{"provider":"existing","auth":"api_key"}`)
	if w := loginCall(s, "DELETE", "/providers/existing", ""); w.Code != 204 {
		t.Fatal("delete failed")
	}
	state = answerLogin(t, s, state, "must-not-save")
	if !state.Done || state.Error == "" || len(reg.Settings.Providers()) != 0 {
		t.Fatalf("sign-in recreated a deleted provider: %+v", state)
	}
	if credential, err := reg.Credentials().Read(t.Context(), providers.Prefix+"existing"); err != nil || credential != nil {
		t.Fatal("removed provider retained a credential")
	}
}

func TestProviderLoginValidation(t *testing.T) {
	t.Parallel()
	s, _ := loginServer(t)
	for _, body := range []string{
		`{"type":"missing","auth":"oauth"}`,
		`{"type":"openai-codex","auth":"api_key"}`,
		`{"provider":"missing","auth":"api_key"}`,
	} {
		if w := loginCall(s, "POST", "/providers/logins", body); w.Code != 400 {
			t.Fatalf("unsupported login accepted: %s", body)
		}
	}
	w := loginCall(s, "GET", "/providers/options", "")
	var options []contract.ProviderOption
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &options) != nil {
		t.Fatal("options unavailable")
	}
	if !slices.ContainsFunc(options, func(option contract.ProviderOption) bool {
		return option.Type == "openai-codex" && option.Auth == "oauth"
	}) {
		t.Fatal("ChatGPT subscription missing")
	}
	r := httptest.NewRequest("POST", "/providers/logins", strings.NewReader(`{"type":"openai","auth":"oauth"}`))
	r.Header.Set("Origin", "https://untrusted.test")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("browser was allowed to start login")
	}
}

func TestProviderLoginSaveFailure(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		t.Run(strconv.FormatBool(existing), func(t *testing.T) {
			s, reg := loginServer(t)
			body := `{"type":"openai","auth":"api_key"}`
			if existing {
				if err := reg.Save([]providers.Provider{{ID: "existing", Type: "openai", Name: "Existing", Enabled: true}}); err != nil {
					t.Fatal(err)
				}
				if _, err := reg.Credentials().Modify(t.Context(), providers.Prefix+"existing", func(*ai.Credential) (*ai.Credential, error) {
					return &ai.Credential{Type: ai.CredentialOAuth, Access: "old-token"}, nil
				}); err != nil {
					t.Fatal(err)
				}
				body = `{"provider":"existing","auth":"api_key"}`
			}
			path := filepath.Join(s.config.DataDir, "settings.json")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			state := startLogin(t, s, body)
			state = answerLogin(t, s, state, "new-key")
			for !state.Done {
				state.Prompt = nil
				state = waitLogin(t, s, state)
			}
			if state.Error == "" || state.Provider != nil {
				t.Fatalf("failed save reported success: %+v", state)
			}
			secrets := reg.Secrets.(providerSecrets)
			if existing {
				credential, err := reg.Credentials().Read(t.Context(), providers.Prefix+"existing")
				if err != nil || credential == nil || credential.Type != ai.CredentialOAuth || credential.Access != "old-token" {
					t.Fatal("previous sign-in lost")
				}
			} else if len(secrets) != 0 {
				t.Fatalf("orphaned credential: %v", secrets)
			}
		})
	}
}

func TestLoginDevicePersists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := loginDevice(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loginDevice(dir)
	if err != nil || first != second || len(first) != 36 || first[14] != '4' {
		t.Fatalf("unstable installation ID: %q %q %v", first, second, err)
	}
}

func TestLoginOAuthEvents(t *testing.T) {
	t.Parallel()
	l := &login{state: contract.ProviderLogin{Events: []contract.LoginEvent{}}, changed: make(chan struct{}), answers: map[int]chan string{}}
	l.notify(ai.AuthURLEvent{URL: "https://auth.test/authorize", Instructions: "Sign in"})
	l.notify(ai.AuthDeviceCodeEvent{UserCode: "ABC-123", VerificationURI: "https://auth.test/device"})
	ctx, cancel := context.WithCancel(t.Context())
	answer := make(chan error, 1)
	go func() {
		_, err := l.ask(ctx, ai.AuthManualCodePrompt{Message: "Paste redirect URL", Placeholder: "http://localhost/callback"})
		answer <- err
	}()
	state := l.snapshot(t.Context(), 2)
	if len(state.Events) != 2 || state.Events[1].UserCode != "ABC-123" || state.Prompt == nil || state.Prompt.Type != "manual_code" {
		t.Fatalf("OAuth interaction lost data: %+v", state)
	}
	cancel()
	if err := <-answer; !errors.Is(err, context.Canceled) {
		t.Fatalf("prompt cancellation: %v", err)
	}
	if state := l.snapshot(t.Context(), -1); state.Prompt != nil {
		t.Fatal("cancelled prompt is still visible")
	}
}
