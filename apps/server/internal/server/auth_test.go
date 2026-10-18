package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/gorilla/websocket"
)

func TestServerAuthentication(t *testing.T) {
	checkErr := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	pairs, err := device.Open(t.Context(), dir, "localhost")
	checkErr(err)
	defer pairs.Close()
	token, err := pairs.Local(t.Context(), dir)
	checkErr(err)
	pair, err := pairs.Authenticate(t.Context(), token)
	checkErr(err)
	broker := device.New(pairs)
	s := settingsAPI(t)
	s.config.Devices = broker
	_, err = s.settings.SetKey("test-only-secret")
	checkErr(err)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
	}))
	defer upstream.Close()
	s.speechDial = func(ctx context.Context, _ string) (*websocket.Conn, error) {
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(upstream.URL, "http"), nil)
		return conn, err
	}
	api := httptest.NewServer(s.Handler())
	defer api.Close()
	check := func(token string, status int) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, api.URL+"/health", nil)
		checkErr(err)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := api.Client().Do(req)
		checkErr(err)
		response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("HTTP status %d, want %d", response.StatusCode, status)
		}
	}
	for _, probe := range []struct{ name, credential, status string }{
		{"unauthenticated", "", "401"}, {"authenticated", token, "200"},
	} {
		cmd := exec.CommandContext(t.Context(), "curl", "--silent", "--show-error", "--output", "/dev/null", "--write-out", "%{http_code}", "--config", "-", api.URL+"/health")
		if probe.credential != "" {
			cmd.Stdin = strings.NewReader("header = \"Authorization: Bearer " + probe.credential + "\"\n")
		}
		output, err := cmd.Output()
		if err != nil || string(output) != probe.status {
			t.Fatalf("%s curl status %s: %v", probe.name, output, err)
		}
		t.Logf("%s curl returns %s", probe.name, output)
	}
	socket := "ws" + strings.TrimPrefix(api.URL, "http") + "/speech/live"
	refused := func(header http.Header) {
		t.Helper()
		conn, response, err := websocket.DefaultDialer.Dial(socket, header)
		if conn != nil {
			conn.Close()
			t.Fatal("unauthorized socket accepted")
		}
		if err == nil || response == nil || response.StatusCode != 401 {
			t.Fatalf("handshake not unauthorized: %v", err)
		}
		response.Body.Close()
	}
	check("", 401)
	refused(nil)
	check(token, 200)
	header := http.Header{"Authorization": {"Bearer " + token}}
	conn, _, err := websocket.DefaultDialer.Dial(socket, header)
	checkErr(err)
	defer conn.Close()
	checkErr(pairs.Revoke(t.Context(), pair.ID))
	check(token, 401)
	refused(header)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("revoked socket remained open")
	} else if failure, ok := err.(interface{ Timeout() bool }); ok && failure.Timeout() {
		t.Fatal("socket only closed by timeout")
	}
}
