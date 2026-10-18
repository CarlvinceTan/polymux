package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/providers"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
)

type providerSecrets map[string]string

func (f providerSecrets) Set(id, key string) error      { f[id] = key; return nil }
func (f providerSecrets) Get(id string) (string, error) { return f[id], nil }
func (f providerSecrets) Delete(id string) error        { delete(f, id); return nil }

func TestProvidersAPI(t *testing.T) {
	prefs, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := providerSecrets{}
	s := &Server{mux: http.NewServeMux()}
	s.providerRoutes(&providers.Registry{Settings: prefs, Secrets: secrets})
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
	call("PUT", "/providers/p/key", `{"key":"private-secret-1234"}`, 200)
	if secrets["p"] != "private-secret-1234" {
		t.Fatal("key not stored")
	}
	if got := call("PUT", "/providers/p/key", `{"key":"private-secret-1234"}`, 200); !strings.Contains(got, "•••• 1234") {
		t.Fatal("missing masked key")
	}
	call("PATCH", "/providers/p", `{"name":"Renamed","enabled":false}`, 200)
	if got := prefs.Providers()[0]; got.Name != "Renamed" || got.Enabled || got.BaseURL != upstream.URL+"/v1" {
		t.Fatal("patch lost metadata")
	}
	call("POST", "/providers/p/test", "", 200)
	var list []providers.Provider
	if err := json.Unmarshal([]byte(call("GET", "/providers", "", 200)), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || len(list[0].Models) != 1 || list[0].Models[0].ID != "text-model" || len(list[0].Models[0].Tags) != 1 || list[0].Models[0].Tags[0] != "text" {
		t.Fatal("discovered models missing")
	}
	for _, tc := range []struct {
		method, path, body, message string
		status                      int
	}{
		{"POST", "/providers", body, "unique IDs", 400},
		{"POST", "/providers", `{"id":"q","type":"custom","name":"Test","baseURL":"https://u:p@host/v1"}`, "without credentials", 400},
		{"POST", "/providers", `{"id":"q","type":"custom","name":"Test","baseURL":"https://u@host/v1"}`, "without credentials", 400},
		{"PATCH", "/providers/p", `{"baseURL":"https://u:p@host/v1"}`, "without credentials", 400},
		{"PATCH", "/providers/p", `{"baseURL":"https://u@host/v1"}`, "without credentials", 400},
		{"PATCH", "/providers/p", `{"baseURL":"file:///tmp/models"}`, "HTTP or HTTPS", 400},
		{"PATCH", "/providers/p", `{"id":"other"}`, "cannot change", 400},
		{"PATCH", "/providers/p", `{"headers":{"Authorization":"secret"}}`, "credentials out of headers", 400},
		{"PUT", "/providers/p/key", `{"key":" "}`, "Enter an API key", 400},
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
	call("DELETE", "/providers/p", "", 204)
	if len(secrets) != 0 || len(prefs.Providers()) != 0 {
		t.Fatal("delete retained metadata or key")
	}
}
