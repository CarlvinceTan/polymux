package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/providers"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
)

type providerSecrets map[string]string

func (f providerSecrets) Set(id, key string) error      { f[id] = key; return nil }
func (f providerSecrets) Get(id string) (string, error) { return f[id], nil }
func (f providerSecrets) Delete(id string) error        { delete(f, id); return nil }

func TestProviderConnectionsAPI(t *testing.T) {
	t.Parallel()
	s, reg := loginServer(t)
	call := func(method, path, body string, status int) providers.Provider {
		t.Helper()
		w := loginCall(s, method, path, body)
		if w.Code != status || strings.Contains(w.Body.String(), "private-key") {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var p providers.Provider
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		return p
	}
	made := call("POST", "/providers", `{"id":"p","type":"custom","name":"Test","baseURL":"https://example.test/v1","enabled":true,"models":[]}`, 200)
	if len(made.Connections) != 1 || made.Connections[0].ID != "p" || !made.Connections[0].Enabled {
		t.Fatalf("a new provider should start with its own connection: %+v", made.Connections)
	}
	first := call("PUT", "/providers/p/connections/p/key", `{"key":"private-key-one-1111"}`, 200)
	if first.Connections[0].KeyHint != "priv••••1111" || first.Connections[0].Status != "api_key" {
		t.Fatalf("missing key metadata: %+v", first.Connections[0])
	}
	added := call("POST", "/providers/p/connections", `{"key":"private-key-two-2222"}`, 200)
	if len(added.Connections) != 2 || added.Connections[1].Name != "API key 2" || !strings.HasPrefix(added.Connections[1].ID, "p~") {
		t.Fatalf("second connection not added: %+v", added.Connections)
	}
	second := added.Connections[1].ID
	if got, _ := reg.Secrets.Get(second); got != "private-key-two-2222" {
		t.Fatal("second key not stored under its connection")
	}
	call("PATCH", "/providers/p/connections/"+second, `{"name":"Spare","enabled":false}`, 200)
	if got := reg.Settings.Providers()[0].Connections[1]; got.Name != "Spare" || got.Enabled {
		t.Fatalf("connection edit lost: %+v", got)
	}
	call("PATCH", "/providers/p", `{"name":"Renamed","connections":[]}`, 200)
	if got := reg.Settings.Providers()[0]; got.Name != "Renamed" || len(got.Connections) != 2 {
		t.Fatal("editing the provider changed its connections")
	}
	// Removing the connection that shares the provider's ID hands that ID to the next one.
	call("DELETE", "/providers/p/connections/p", "", 200)
	left := reg.Settings.Providers()[0].Connections
	if len(left) != 1 || left[0].ID != "p" {
		t.Fatalf("the remaining connection should take the provider's ID: %+v", left)
	}
	if got, _ := reg.Secrets.Get("p"); got != "private-key-two-2222" {
		t.Fatal("the remaining connection's key did not move with it")
	}
	if got, _ := reg.Secrets.Get(second); got != "" {
		t.Fatal("the old key was left behind")
	}
	call("DELETE", "/providers/p/connections/p", "", 409)
	call("PUT", "/providers/p/connections/missing/key", `{"key":"k"}`, 404)
	call("DELETE", "/providers/p", "", 204)
}

func TestProvidersAPI(t *testing.T) {
	prefs, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := providerSecrets{}
	s := &Server{mux: http.NewServeMux()}
	s.providerRoutes(&providers.Registry{Settings: prefs, Secrets: secrets}, &sync.Mutex{})
	call := func(method, path, body string, status int) string {
		t.Helper()
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-secret-1234") {
			t.Fatal("response leaked key")
		}
		return w.Body.String()
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer private-secret-1234" || r.Header.Get("X-Test") != "yes" {
			t.Error("incorrect discovery request")
		}
		w.Write([]byte(`{"data":[{"id":"text-model","tags":["text"]}]}`))
	}))
	defer upstream.Close()
	body := `{"id":"p","type":"custom","name":"Test","baseURL":"` + upstream.URL + `/v1","headers":{"X-Test":"yes"},"enabled":true}`
	call("POST", "/providers", body, 200)
	if got := call("PUT", "/providers/p/connections/p/key", `{"key":"private-secret-1234"}`, 200); !strings.Contains(got, "priv••••1234") {
		t.Fatal("missing masked key")
	}
	if secrets["p"] != "private-secret-1234" {
		t.Fatal("key not stored")
	}
	call("PUT", "/providers/p/connections/p/key", `{"key":"replacement-secret"}`, 200)
	if secrets["p"] != "replacement-secret" {
		t.Fatal("a new key did not replace the old one")
	}
	call("PUT", "/providers/p/connections/p/key", `{"key":"private-secret-1234"}`, 200)
	call("PATCH", "/providers/p", `{"name":"Renamed","enabled":false}`, 200)
	if got := prefs.Providers()[0]; got.Name != "Renamed" || got.Enabled || got.BaseURL != upstream.URL+"/v1" {
		t.Fatal("patch lost metadata")
	}
	call("POST", "/providers/p/test", "", 200)
	var list []providers.Provider
	if err := json.Unmarshal([]byte(call("GET", "/providers", "", 200)), &list); err != nil {
		t.Fatal(err)
	}
	if list[0].Connections[0].KeyHint != "priv••••1234" || prefs.Providers()[0].Connections[0].KeyHint != "" {
		t.Fatal("key hint missing from response or persisted with settings")
	}
	if len(list) != 1 || len(list[0].Models) != 1 || list[0].Models[0].ID != "text-model" || len(list[0].Models[0].Tags) != 1 || list[0].Models[0].Tags[0] != "text" {
		t.Fatal("discovered models missing")
	}
	for _, tc := range []struct {
		method, path, body, message string
		status                      int
	}{
		{"POST", "/providers", body, "already exists", 409},
		{"POST", "/providers", `{"id":"q","type":"custom","name":"Test","baseURL":"https://u:p@host/v1"}`, "without credentials", 400},
		{"POST", "/providers", `{"id":"q","type":"custom","name":"Test","baseURL":"https://u@host/v1"}`, "without credentials", 400},
		{"PATCH", "/providers/p", `{"baseURL":"https://u:p@host/v1"}`, "without credentials", 400},
		{"PATCH", "/providers/p", `{"baseURL":"https://u@host/v1"}`, "without credentials", 400},
		{"PATCH", "/providers/p", `{"baseURL":"file:///tmp/models"}`, "HTTP or HTTPS", 400},
		{"PATCH", "/providers/p", `{"id":"other"}`, "cannot change", 400},
		{"PATCH", "/providers/p", `{"headers":{"Authorization":"secret"}}`, "credentials out of headers", 400},
		{"PUT", "/providers/p/connections/p/key", `{"key":" "}`, "Enter an API key", 400},
		{"POST", "/providers", `{"key":"secret"}`, "Invalid JSON", 400},
		{"DELETE", "/providers/missing", "", "not found", 404},
	} {
		if got := call(tc.method, tc.path, tc.body, tc.status); !strings.Contains(got, tc.message) {
			t.Fatalf("missing specific error: %s", got)
		}
	}
	if got := prefs.Providers(); len(got) != 1 || got[0].BaseURL != upstream.URL+"/v1" {
		t.Fatalf("rejected URLs changed the saved provider: %+v", got)
	}
	call("PATCH", "/providers/p", `{"enabled":true}`, 200)
	empty, model := "", "text-model"
	provider := "p"
	if _, err := prefs.Update(contract.SettingsPatch{DefaultProvider: &provider, DefaultModel: &model}); err != nil {
		t.Fatal(err)
	}
	call("DELETE", "/providers/p", "", 204)
	if got := prefs.Snapshot(); got.DefaultProvider != empty || got.DefaultModel != empty {
		t.Fatalf("deleting the default provider kept it as the default: %+v", got)
	}
	if len(secrets) != 0 || len(prefs.Providers()) != 0 {
		t.Fatal("delete retained metadata or key")
	}
}

func TestBuiltinModelsUsePiG(t *testing.T) {
	for _, kind := range []string{"anthropic", "gemini", "openrouter", "openai"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			prefs, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			s := &Server{mux: http.NewServeMux()}
			s.providerRoutes(&providers.Registry{Settings: prefs, Secrets: providerSecrets{}}, &sync.Mutex{})
			steps := []struct {
				method, path, body string
				status             int
			}{
				{method: "POST", path: "/providers", body: `{"id":"p","type":"` + kind + `","name":"P","baseURL":"https://ignored.test","enabled":true,"models":[]}`, status: 200},
				{method: "POST", path: "/providers/p/test", status: 502},
				{method: "PUT", path: "/providers/p/connections/p/key", body: `{"key":"k"}`, status: 200},
				{method: "POST", path: "/providers/p/test", status: 200},
			}
			for _, step := range steps {
				w := httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest(step.method, step.path, strings.NewReader(step.body)))
				if w.Code != step.status {
					t.Fatalf("%s %s: %d %s", step.method, step.path, w.Code, w.Body.String())
				}
			}
			got := prefs.Providers()[0]
			builtin := providers.Builtin(kind)
			if got.BaseURL != builtin.BaseURL || len(got.Models) == 0 {
				t.Fatalf("missing PiG endpoint/models: %+v", got)
			}
		})
	}
}

type providerProbe struct {
	Factory
	specs []agentruntime.ProviderSpec
}

func (p *providerProbe) SetProviders(specs []agentruntime.ProviderSpec) { p.specs = specs }

func TestProviderOrder(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	prefs, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	probe := &providerProbe{}
	s := &Server{mux: http.NewServeMux(), config: Config{Factory: probe}}
	reg := &providers.Registry{Settings: prefs, Secrets: providerSecrets{}}
	s.providerRoutes(reg, &sync.Mutex{})
	if err := reg.Save([]providers.Provider{
		{ID: "second", Type: "custom", Name: "Second", Order: 8, Enabled: true, Connections: []providers.Connection{{ID: "second", Enabled: true}}, Models: []providers.Model{{ID: "image", Enabled: true, Tags: []string{"image-gen"}}, {ID: "chat", Enabled: true, Tags: []string{"text"}}}},
		{ID: "first", Type: "custom", Name: "First", Order: 1, Enabled: true, Connections: []providers.Connection{{ID: "first", Enabled: true}}},
		{ID: "off", Type: "custom", Name: "Off", Order: 2, Connections: []providers.Connection{{ID: "off", Enabled: true}}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := prefs.Providers(); got[0].ID != "first" || got[1].ID != "off" || got[2].ID != "second" {
		t.Fatalf("saved priority ignored: %+v", got)
	}
	w := loginCall(s, "PUT", "/providers/order", `["second","off","first"]`)
	if w.Code != 200 {
		t.Fatalf("reorder: %d %s", w.Code, w.Body.String())
	}
	if len(probe.specs) != 2 || probe.specs[0].ID != "second" || probe.specs[1].ID != "first" || len(probe.specs[0].Models) != 1 || probe.specs[0].Models[0] != "chat" {
		t.Fatalf("runtime priority or chat filtering lost: %+v", probe.specs)
	}
	reopened, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, provider := range reopened.Providers() {
		if provider.ID != []string{"second", "off", "first"}[i] || provider.Order != i {
			t.Fatal("reordered providers did not persist")
		}
	}
	for _, body := range []string{`["second","first"]`, `["second","second","first"]`, `["second","missing","first"]`} {
		w = loginCall(s, "PUT", "/providers/order", body)
		if w.Code != 409 || prefs.Providers()[0].ID != "second" {
			t.Fatal("stale reorder changed provider metadata")
		}
	}
	w = loginCall(s, "POST", "/providers", `{"id":"new","type":"custom","name":"New","baseURL":"http://localhost/v1","enabled":true}`)
	if w.Code != 200 || prefs.Providers()[3].ID != "new" {
		t.Fatalf("new connection didn't append: %d %s", w.Code, w.Body.String())
	}
}
