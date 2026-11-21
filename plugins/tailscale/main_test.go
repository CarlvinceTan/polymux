package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeCLI is a tailscale command that logs its arguments and answers from files in dir.
func fakeCLI(t *testing.T, status string) (cli, dir string) {
	t.Helper()
	dir = t.TempDir()
	cli = filepath.Join(dir, "tailscale")
	script := "#!/bin/sh\necho \"$@\" >> " + dir + "/log\n" +
		"case \"$1\" in\nstatus) cat " + dir + "/status ;;\n" +
		"serve) if [ -f " + dir + "/fail ]; then echo 'Serve is not enabled on your tailnet.' >&2; echo 'Visit https://login.tailscale.com' >&2; exit 1; fi ;;\nesac\n"
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
	return cli, dir
}

func newService(cli string, options settings) *service {
	return &service{cli: cli, api: "127.0.0.1:7341", device: "127.0.0.1:7339", options: options,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)), current: report{State: "starting"}}
}

const running = `{"BackendState":"Running","Self":{"DNSName":"mini.tail1234.ts.net."}}`

func logged(t *testing.T, dir string) []string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(dir, "log"))
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestReconcilePublishesBothListeners(t *testing.T) {
	cli, dir := fakeCLI(t, running)
	got := newService(cli, settings{HTTPSPort: "8443", DevicePort: "7339"}).reconcile(context.Background())
	want := report{State: "ready", API: "https://mini.tail1234.ts.net:8443", Device: "https://mini.tail1234.ts.net:7339"}
	if got != want {
		t.Fatalf("report %+v, want %+v", got, want)
	}
	calls := logged(t, dir)
	for _, call := range []string{"serve --bg --https=8443 http://127.0.0.1:7341", "serve --bg --tcp=7339 tcp://127.0.0.1:7339"} {
		if !slices.Contains(calls, call) {
			t.Errorf("never ran %q: %v", call, calls)
		}
	}
}

func TestStandardPortStaysImplicit(t *testing.T) {
	cli, _ := fakeCLI(t, running)
	got := newService(cli, settings{HTTPSPort: "443", DevicePort: "7339"}).reconcile(context.Background())
	if got.API != "https://mini.tail1234.ts.net" {
		t.Fatalf("api %q", got.API)
	}
}

func TestReconcileExplainsWhatIsWrong(t *testing.T) {
	for name, test := range map[string]struct{ status, message string }{
		"signed out": {`{"BackendState":"NeedsLogin"}`, "Sign in to Tailscale."},
		"stopped":    {`{"BackendState":"Stopped"}`, "Tailscale isn't running."},
		"garbage":    {`not json`, "Tailscale isn't running."},
		"no name":    {`{"BackendState":"Running","Self":{"DNSName":""}}`, "Turn on MagicDNS in your Tailscale settings."},
	} {
		cli, _ := fakeCLI(t, test.status)
		got := newService(cli, settings{HTTPSPort: "8443", DevicePort: "7339"}).reconcile(context.Background())
		if got.State != "error" || got.Message != test.message {
			t.Errorf("%s: %+v", name, got)
		}
	}
	missing := newService(filepath.Join(t.TempDir(), "absent"), settings{}).reconcile(context.Background())
	if missing.State != "error" || missing.Message != "Tailscale isn't installed." {
		t.Errorf("missing cli: %+v", missing)
	}
	cli, dir := fakeCLI(t, running)
	if err := os.WriteFile(filepath.Join(dir, "fail"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	failed := newService(cli, settings{HTTPSPort: "8443", DevicePort: "7339"}).reconcile(context.Background())
	if failed.State != "error" || failed.Message != "Serve is not enabled on your tailnet." || failed.API != "" {
		t.Errorf("serve failure: %+v", failed)
	}
}

func TestWithdrawTurnsListenersOff(t *testing.T) {
	cli, dir := fakeCLI(t, running)
	newService(cli, settings{HTTPSPort: "8443", DevicePort: "7339"}).withdraw()
	calls := logged(t, dir)
	if !slices.Contains(calls, "serve --https=8443 off") || !slices.Contains(calls, "serve --tcp=7339 off") {
		t.Fatalf("calls %v", calls)
	}
}

func TestTransportNeedsTheToken(t *testing.T) {
	s := newService("unused", settings{})
	s.current = report{State: "ready", API: "https://a.example"}
	handler := s.handler("secret")
	for token, want := range map[string]int{"": 401, "wrong": 401, "secret": 200} {
		request := httptest.NewRequest("GET", "/transport", nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != want {
			t.Errorf("token %q answered %d", token, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/transport", nil)
	request.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(recorder, request)
	var got report
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil || got.API != "https://a.example" {
		t.Fatalf("body %v %+v", err, got)
	}
	health := httptest.NewRecorder()
	healthRequest := httptest.NewRequest("GET", "/health", nil)
	healthRequest.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(health, healthRequest)
	if health.Code != http.StatusOK {
		t.Fatalf("health %d", health.Code)
	}
}

func TestSettingsKeepValidPortsOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"https-port":"9443","device-port":"0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readSettings(path); got.HTTPSPort != "9443" || got.DevicePort != "7339" {
		t.Fatalf("settings %+v", got)
	}
	if got := readSettings(filepath.Join(t.TempDir(), "absent")); got.HTTPSPort != "8443" || got.DevicePort != "7339" {
		t.Fatalf("defaults %+v", got)
	}
}
