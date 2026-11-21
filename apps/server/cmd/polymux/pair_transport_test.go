package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPairCandidatesUseTransports(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "devices"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devices", "local-token"), []byte("owner\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transports" || r.Header.Get("Authorization") != "Bearer owner" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`[{"state":"ready","api":"https://a.example","device":"https://a.example:7339"},` +
			`{"state":"error","device":"https://broken.example"},{"state":"ready","api":"https://api-only.example"}]`))
	}))
	defer server.Close()
	api := server.Listener.Addr().String()
	got := pairCandidates(t.Context(), dir, api, []string{"https://typed.example"})
	if want := []string{"https://a.example:7339", "https://typed.example"}; !slices.Equal(got, want) {
		t.Fatalf("candidates %v, want %v", got, want)
	}
	if got := pairCandidates(t.Context(), dir, "127.0.0.1:1", []string{"https://typed.example"}); !slices.Equal(got, []string{"https://typed.example"}) {
		t.Fatalf("an unreachable server should leave only typed addresses: %v", got)
	}
}
