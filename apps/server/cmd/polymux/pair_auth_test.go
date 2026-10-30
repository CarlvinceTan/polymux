package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
)

func TestPairAuthorization(t *testing.T) {
	dir := t.TempDir()
	const owner = "owner-token-fixture"
	if err := os.MkdirAll(filepath.Join(dir, "devices"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devices", "local-token"), []byte(owner), 0600); err != nil {
		t.Fatal(err)
	}
	tsMu.Lock()
	oldAt, oldURL := tsAt, tsURL
	tsAt, tsURL = time.Now(), ""
	tsMu.Unlock()
	defer func() { tsMu.Lock(); tsAt, tsURL = oldAt, oldURL; tsMu.Unlock() }()
	for _, test := range []struct {
		name, remote, origin, token string
		want                        int
	}{
		{"non-loopback", "192.0.2.50:1234", "", owner, 403},
		{"browser", "127.0.0.1:1234", "https://example.invalid", owner, 403},
		{"loopback-no-credential", "127.0.0.1:1234", "", "", 403},
		{"loopback-wrong-credential", "127.0.0.1:1234", "", "not-the-owner", 403},
		{"owner", "127.0.0.1:1234", "", owner, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/device/pair", strings.NewReader(`{"agent":"audit-placeholder","address":"https://pair.invalid:7339"}`))
			r.RemoteAddr = test.remote
			r.Header.Set("Origin", test.origin)
			if test.token != "" {
				r.Header.Set("Authorization", "Bearer "+test.token)
			}
			w := httptest.NewRecorder()
			servePair(w, r, dir, "7339", false)
			if w.Code != test.want {
				t.Fatalf("pairing status %d, want %d", w.Code, test.want)
			}
			if w.Code != 200 {
				return
			}
			var result struct{ URL string }
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			link, err := url.Parse(result.URL)
			if err != nil || len(link.Query().Get("c")) != 43 {
				t.Fatal("pairing response missing a valid fixture code")
			}
			pairs, err := device.Open(t.Context(), dir, "localhost")
			if err != nil {
				t.Fatal(err)
			}
			defer pairs.Close()
			broker := device.New(pairs)
			redeem := httptest.NewRequest("POST", "/redeem", strings.NewReader(`{"code":"`+link.Query().Get("c")+`"}`))
			redeem.TLS = &tls.ConnectionState{}
			enrolled := httptest.NewRecorder()
			broker.ServeHTTP(enrolled, redeem)
			if enrolled.Code != 200 {
				t.Fatalf("fixture enrollment status %d", enrolled.Code)
			}
			var pair device.Enrollment
			if err := json.Unmarshal(enrolled.Body.Bytes(), &pair); err != nil {
				t.Fatal(err)
			}
			read := httptest.NewRequest("GET", "/audit-read", nil)
			read.Header.Set("Authorization", "Bearer "+pair.Token)
			response := httptest.NewRecorder()
			broker.Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(204)
			})).ServeHTTP(response, read)
			if response.Code != 204 {
				t.Fatal("enrolled fixture credential failed harmless protected read")
			}
		})
	}
}
