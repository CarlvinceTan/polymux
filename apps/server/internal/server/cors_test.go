package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppOrigin(t *testing.T) {
	allowed := []string{
		"tauri://localhost",
		"http://tauri.localhost",
		"http://127.0.0.1:1420",
		"http://localhost:1420",
		"http://[::1]:1420",
	}
	refused := []string{"", "null", "https://example.com", "http://127.0.0.1.example.com:1420"}
	for _, origin := range allowed {
		if !appOrigin(origin) {
			t.Errorf("appOrigin(%q) = false, want true", origin)
		}
	}
	for _, origin := range refused {
		if appOrigin(origin) {
			t.Errorf("appOrigin(%q) = true, want false", origin)
		}
	}
}

func TestApplyCORS(t *testing.T) {
	preflight := httptest.NewRequest(http.MethodOptions, "/agents", nil)
	preflight.Header.Set("Origin", "tauri://localhost")
	preflight.Header.Set("Access-Control-Request-Method", "PATCH")
	preflight.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	response := httptest.NewRecorder()
	if applyCORS(response, preflight) {
		t.Fatal("applyCORS served the preflight request")
	}
	if got := response.Code; got != http.StatusNoContent {
		t.Errorf("preflight status = %d, want %d", got, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "tauri://localhost" {
		t.Errorf("allow origin = %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("preflight is missing the allowed methods")
	}

	read := httptest.NewRequest(http.MethodGet, "/agents", nil)
	read.Header.Set("Origin", "http://127.0.0.1:1420")
	response = httptest.NewRecorder()
	if !applyCORS(response, read) {
		t.Fatal("applyCORS rejected the request")
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:1420" {
		t.Errorf("allow origin = %q", got)
	}

	stranger := httptest.NewRequest(http.MethodGet, "/agents", nil)
	stranger.Header.Set("Origin", "https://example.com")
	response = httptest.NewRecorder()
	if !applyCORS(response, stranger) {
		t.Fatal("applyCORS rejected the request")
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allow origin = %q, want none", got)
	}
}
