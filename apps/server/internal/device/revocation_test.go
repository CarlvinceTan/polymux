package device

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRevokedStream(t *testing.T) {
	broker, store, _, pair := fixture(t)
	closed := make(chan struct{})
	server := httptest.NewServer(broker.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: synthetic-initial\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	})))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+pair.Token)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("synthetic stream was not authenticated", response.StatusCode)
	}
	if err := store.Revoke(t.Context(), pair.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
		t.Log("revocation cancelled the established authenticated HTTP stream")
	case <-time.After(750 * time.Millisecond):
		t.Fatal("revoked device's established authenticated HTTP stream remains open")
	}
}

func TestCrossStoreRevocation(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	first.recheck = 50 * time.Millisecond
	second, err := Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	token, err := first.IssueAppToken(t.Context(), "Audit fixture")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := first.Authenticate(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	started, ended := make(chan struct{}), make(chan struct{})
	handler := New(first).Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(ended)
	}))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/audit-stream", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+token)
	go handler.ServeHTTP(httptest.NewRecorder(), request)
	<-started
	defer func() { cancel(); <-ended }()
	if err := second.Revoke(t.Context(), pair.ID); err != nil {
		t.Fatal(err)
	}
	fresh := httptest.NewRecorder()
	New(first).Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("revoked credential reached protected handler")
	})).ServeHTTP(fresh, request)
	if fresh.Code != http.StatusUnauthorized {
		t.Fatalf("fresh request status %d, want 401", fresh.Code)
	}
	select {
	case <-ended:
		t.Log("separate-store revocation cancelled the active request")
	case <-time.After(time.Second):
		t.Error("separate-store revocation rejected fresh requests but did not cancel the established request")
	}
}
