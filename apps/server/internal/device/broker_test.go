package device

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*Broker, *Store, *httptest.Server, Enrollment) {
	t.Helper()
	s := openStore(t)
	b := New(s)
	server := httptest.NewUnstartedServer(b)
	server.TLS = s.TLSConfig()
	server.TLS.Certificates = []tls.Certificate{s.cert}
	server.StartTLS()
	t.Cleanup(server.Close)
	token, err := s.IssueAppToken(t.Context(), "Test app")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := s.Authenticate(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	return b, s, server, Enrollment{Pairing: pair, Token: token}
}

func redeem(b *Broker, code string, secure bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/redeem", strings.NewReader(`{"code":"`+code+`"}`))
	if secure {
		r.TLS = &tls.ConnectionState{}
	}
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w
}

func TestRedeemEnrollsApp(t *testing.T) {
	t.Parallel()
	b, s, _, _ := fixture(t)
	code := codeOf(t, appCode(t, s, []string{"https://mac.example:7339"}))
	w := redeem(b, code, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var enrolled Enrollment
	if json.Unmarshal(w.Body.Bytes(), &enrolled) != nil || enrolled.ID == "" || len(enrolled.Token) != 43 {
		t.Fatal("no app enrollment")
	}
	if _, err := s.Authenticate(t.Context(), enrolled.Token); err != nil {
		t.Fatal(err)
	}
	all, err := b.All(t.Context())
	if err != nil || len(all) != 2 {
		t.Fatalf("devices=%d err=%v", len(all), err)
	}
	if err := s.Revoke(t.Context(), enrolled.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := b.All(t.Context())
	if err != nil || len(listed) != 1 {
		t.Fatalf("revoked device listed: %d err=%v", len(listed), err)
	}
	if replay := redeem(b, code, true); replay.Code != http.StatusForbidden {
		t.Fatal("replayed code enrolled")
	}
}

func TestDeviceIngress(t *testing.T) {
	t.Parallel()
	b := New(openStore(t))
	for _, test := range []struct {
		name   string
		method string
		path   string
		code   string
		origin string
		tls    bool
		want   int
	}{
		{"plaintext", "POST", "/redeem", "code", "", false, 403},
		{"browser", "POST", "/redeem", "code", "https://example.com", true, 403},
		{"no-code", "POST", "/redeem", "", "", true, 403},
		{"unknown", "GET", "/unknown", "", "", true, 404},
		{"enroll", "POST", "/enroll", "code", "", true, 404},
		{"connect", "GET", "/connect", "", "", true, 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(test.method, "https://127.0.0.1"+test.path, strings.NewReader(`{"code":"`+test.code+`"}`))
			if test.tls {
				r.TLS = &tls.ConnectionState{}
			}
			r.Header.Set("Origin", test.origin)
			w := httptest.NewRecorder()
			b.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status=%d, want %d", w.Code, test.want)
			}
		})
	}
}
