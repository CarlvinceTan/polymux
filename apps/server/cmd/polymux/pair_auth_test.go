package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
)

func TestEnrolledDeviceReachesProtectedRoutes(t *testing.T) {
	pairs, err := device.Open(t.Context(), t.TempDir(), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer pairs.Close()
	issued, err := pairs.IssueApp(t.Context(), []string{"https://paired.example.invalid:7339"}, "audit-placeholder")
	if err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(issued)
	if err != nil || len(link.Query().Get("c")) != 43 {
		t.Fatal("pairing response missing a valid fixture code")
	}
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
}
