package device

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInviteHTTPBoundary(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir(), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	code, _ := s.IssueInvite(t.Context())
	token := randomID()
	server := httptest.NewServer(New(s).Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })))
	defer server.Close()
	call := func(path, body, origin, credential string, native bool) int {
		t.Helper()
		method := "GET"
		if body != "" {
			method = "POST"
		}
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if credential != "" {
			r.Header.Set("Authorization", "Bearer "+credential)
		}
		if native {
			r.Header.Set("X-Polymux-Client", "polymux-kit")
		}
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	data, _ := json.Marshal(map[string]string{"code": code, "token": token})
	body := string(data)
	if got := call("/health", "", "", code, true); got != 401 {
		t.Fatal("invitation bypassed API authentication:", got)
	}
	for _, test := range []struct {
		origin string
		native bool
	}{{"https://evil.example", true}, {"", false}} {
		if got := call("/network/pair", body, test.origin, "", test.native); got == 204 {
			t.Fatal("browser/non-native request redeemed an invitation")
		}
	}
	if got := call("/network/pair", body+`{}`, "", "", true); got == 204 {
		t.Fatal("trailing JSON was accepted")
	}
	for range 2 {
		if got := call("/network/pair", body, "", "", true); got != 204 {
			t.Fatal("claim/retry failed:", got)
		}
	}
	if got := call("/health", "", "", token, true); got != 200 {
		t.Fatal("new device cannot use the API:", got)
	}
	if got := call("/health", "", "", code, true); got != 401 {
		t.Fatal("used invitation became an API credential:", got)
	}
	other, _ := json.Marshal(map[string]string{"code": code, "token": randomID()})
	if got := call("/network/pair", string(other), "", "", true); got != 409 {
		t.Fatal("another device reused the claim:", got)
	}
}
