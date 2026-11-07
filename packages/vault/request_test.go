package vault

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRequestSecrecy(t *testing.T) {
	s, _, _ := testStore(t)
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var fields map[string]string
		if json.NewDecoder(r.Body).Decode(&fields) != nil || fields["password"] != "hidden-value-7ce2" {
			t.Error("secret was not injected")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, http.StatusFound)
			return
		}
		w.Header().Set("Set-Cookie", "session=server-session-secret")
		w.Header().Set("X-Echo", fields["password"])
		fmt.Fprint(w, fields["password"]+base64.StdEncoding.EncodeToString([]byte(fields["password"])))
	}))
	defer origin.Close()
	item := testItem()
	item.URL = origin.URL
	if _, err := s.Save(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/echo", "/redirect"} {
		result, err := s.Request(t.Context(), Request{ID: item.ID, URL: origin.URL + path, Fields: map[string]string{"password": "password"}})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(result)
		if strings.Contains(string(data), "secret") || strings.Contains(string(data), "hidden") || len(data) > 32 {
			t.Fatal("response leaked content")
		}
		if path == "/redirect" && result.Status != 302 {
			t.Fatal("redirect was followed")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("cross-origin redirect received a request")
	}
	if _, err := s.Request(t.Context(), Request{ID: item.ID, URL: other.URL, Fields: map[string]string{"password": "password"}}); err == nil {
		t.Fatal("origin bypass")
	}
}

func TestOriginValidation(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"case and port", "https://EXAMPLE.com:443/login?return=/x", "https://example.com"},
		{"local", "http://127.0.0.1:9182/login", "http://127.0.0.1:9182"},
		{"ipv6", "https://[::1]:443/login", "https://[::1]"},
		{"plain network", "http://example.com", ""},
		{"inline credential", "https://user:pass@example.com", ""},
		{"script", "javascript:alert(1)", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Origin(tc.raw)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatal("origin validation mismatch")
			}
		})
	}
}
