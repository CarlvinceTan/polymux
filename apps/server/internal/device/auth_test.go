package device

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func apiFixture(t *testing.T) (*Store, *httptest.Server, Enrollment) {
	t.Helper()
	b, store, _, pair := fixture(t)
	api := httptest.NewServer(b.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/socket" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})))
	t.Cleanup(api.Close)
	return store, api, pair
}

func TestUnpairedHTTPRejected(t *testing.T) {
	_, api, _ := apiFixture(t)
	for _, header := range []string{"", "invalid", "Bearer invalid"} {
		request, _ := http.NewRequest(http.MethodGet, api.URL+"/health", nil)
		request.Header.Set("Authorization", header)
		response, err := api.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status: %d", response.StatusCode)
		}
	}
}

func TestUnpairedWebSocketClosed(t *testing.T) {
	_, api, _ := apiFixture(t)
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(api.URL, "http")+"/socket", nil)
	if conn != nil {
		conn.Close()
		t.Fatal("unpaired socket accepted")
	}
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized handshake: %v", err)
	}
	response.Body.Close()
}

func TestPairedRequestSucceeds(t *testing.T) {
	_, api, pair := apiFixture(t)
	request, _ := http.NewRequest(http.MethodGet, api.URL+"/health", nil)
	request.Header.Set("Authorization", "Bearer "+pair.Token)
	response, err := api.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status: %d", response.StatusCode)
	}
}

func TestRevokedDeviceRejectedIncludingOpenSocket(t *testing.T) {
	store, api, pair := apiFixture(t)
	header := http.Header{"Authorization": []string{"Bearer " + pair.Token}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(api.URL, "http")+"/socket", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := store.Revoke(t.Context(), pair.ID); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("revoked socket remained open")
	} else if failure, ok := err.(interface{ Timeout() bool }); ok && failure.Timeout() {
		t.Fatal("socket only closed by timeout")
	}
	request, _ := http.NewRequest(http.MethodGet, api.URL+"/health", nil)
	request.Header = header
	response, err := api.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked status: %d", response.StatusCode)
	}
}
