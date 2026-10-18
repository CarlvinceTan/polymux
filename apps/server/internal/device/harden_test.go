package device

import (
	"crypto/tls"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBurnedOnFirstUse(t *testing.T) {
	s := openStore(t)
	code := codeOf(t, appCode(t, s, []string{"https://mac.example"}))
	if _, err := s.RedeemApp(t.Context(), "a", code); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemApp(t.Context(), "b", code); err == nil {
		t.Fatal("reused code")
	}
}

func TestGlobalHandshakeRateLimit(t *testing.T) {
	var l Limiter
	now := time.Now()
	for range 20 {
		if !l.Allow("a", now) {
			t.Fatal("early limit")
		}
	}
	if l.Allow("a", now) {
		t.Fatal("unlimited source")
	}
	if !l.Allow("b", now) || !l.Allow("a", now.Add(time.Minute)) {
		t.Fatal("source isolation or expiry")
	}
	r := httptest.NewRequest("POST", "/redeem", nil)
	r.RemoteAddr = "127.0.0.1:4444"
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	if Caller(r) != "route:device" {
		t.Fatal("untrusted caller")
	}
}

func TestPrePairRevealsNothing(t *testing.T) {
	b := New(openStore(t))
	for _, path := range []string{"/", "/enroll", "/connect", "/pairing", "/version", "/unknown"} {
		r := httptest.NewRequest("POST", path, strings.NewReader("{}"))
		r.TLS = &tls.ConnectionState{}
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		if w.Code != 404 || strings.Contains(w.Body.String(), "token") {
			t.Fatalf("%s leaked: %d %q", path, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "/redeem", strings.NewReader(`{"code":"unknown"}`))
	r.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 403 || w.Body.String() != string(make([]byte, 64)) {
		t.Fatalf("/redeem leaked: %d %q", w.Code, w.Body.String())
	}
}

func TestRedeemRequiresTLS(t *testing.T) {
	s := openStore(t)
	b := New(s)
	code := codeOf(t, appCode(t, s, []string{"https://mac.example"}))
	if w := redeem(b, code, false); w.Code != 403 {
		t.Fatalf("plaintext status: %d", w.Code)
	}
	if _, err := s.RedeemApp(t.Context(), "route:device", code); err != nil {
		t.Fatal("plaintext request consumed the code")
	}
}
