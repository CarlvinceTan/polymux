package server

import (
	"net/http"
	"strings"
)

// appOrigin reports whether a web view origin belongs to an app on this machine.
func appOrigin(origin string) bool {
	switch origin {
	case "tauri://localhost", "http://tauri.localhost", "https://tauri.localhost":
		return true
	}
	for _, prefix := range []string{"http://127.0.0.1:", "http://localhost:", "http://[::1]:"} {
		if strings.HasPrefix(origin, prefix) {
			return true
		}
	}
	return false
}

// AllowAppOrigins wraps a handler so app web views can read its responses.
func AllowAppOrigins(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if applyCORS(w, r) {
			next.ServeHTTP(w, r)
		}
	})
}

// applyCORS lets an app's web view read responses from its own origin.
func applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || !appOrigin(origin) {
		return true
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	if r.Method != http.MethodOptions {
		return true
	}
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Polymux-Client")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
	return false
}
