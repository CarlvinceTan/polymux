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

// fakeCLI is an opentunnel command that logs its arguments and prints out.
func fakeCLI(t *testing.T, out string) (cli, dir string) {
	t.Helper()
	dir = t.TempDir()
	cli = filepath.Join(dir, "opentunnel")
	script := "#!/bin/sh\necho \"$@\" >> " + dir + "/log\n" +
		"if [ -f " + dir + "/fail ]; then echo 'Error: no such host' >&2; exit 1; fi\n" +
		"cat " + dir + "/out\n"
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out"), []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	return cli, dir
}

func newService(cli string, options settings) *service {
	return &service{cli: cli, api: "127.0.0.1:7341", options: options,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)), current: report{State: "starting"}}
}

const routeURL = "https://polymux.abcdefghijkl.opentunnel.xyz"

func logged(t *testing.T, dir string) []string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(dir, "log"))
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestReconcilePublishesRoute(t *testing.T) {
	cli, dir := fakeCLI(t, "Added route polymux → 127.0.0.1:7341\n"+routeURL+"\n")
	got := newService(cli, settings{Profile: "polymux", Route: "polymux"}).reconcile(context.Background())
	want := report{State: "ready", API: routeURL}
	if got != want {
		t.Fatalf("report %+v, want %+v", got, want)
	}
	if calls := logged(t, dir); !slices.Contains(calls, "--profile polymux route add 127.0.0.1:7341 --name polymux") {
		t.Fatalf("calls %v", calls)
	}
}

func TestServerAndCAReachTheCLI(t *testing.T) {
	cli, dir := fakeCLI(t, routeURL+"\n")
	newService(cli, settings{Profile: "work", Route: "@"}).reconcile(context.Background())
	if calls := logged(t, dir); !slices.Contains(calls, "--profile work route add 127.0.0.1:7341 --name @") {
		t.Fatalf("calls %v", calls)
	}
	cli, dir = fakeCLI(t, routeURL+"\n")
	newService(cli, settings{Profile: "work", Route: "api", Server: "https://tunnel.example", CA: "/tmp/ca.pem"}).reconcile(context.Background())
	if calls := logged(t, dir); !slices.Contains(calls, "--profile work --api https://tunnel.example --ca-cert /tmp/ca.pem route add 127.0.0.1:7341 --name api") {
		t.Fatalf("calls %v", calls)
	}
}

func TestReconcileExplainsWhatIsWrong(t *testing.T) {
	missing := newService("", settings{})
	missing.find = func() string { return "" }
	if got := missing.reconcile(context.Background()); got.State != "error" || got.Message != "OpenTunnel isn't installed." {
		t.Errorf("missing cli: %+v", got)
	}
	cli, dir := fakeCLI(t, "")
	if err := os.WriteFile(filepath.Join(dir, "fail"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	failed := newService(cli, settings{Profile: "polymux", Route: "polymux"}).reconcile(context.Background())
	if failed.State != "error" || failed.Message != "no such host" || failed.API != "" {
		t.Errorf("failed command: %+v", failed)
	}
	cli, _ = fakeCLI(t, "Creating tunnel...\n")
	waiting := newService(cli, settings{Profile: "polymux", Route: "polymux"}).reconcile(context.Background())
	if waiting.State != "starting" || waiting.API != "" {
		t.Errorf("waiting: %+v", waiting)
	}
}

func TestWithdrawRemovesOnlyTheRoute(t *testing.T) {
	cli, dir := fakeCLI(t, "")
	newService(cli, settings{Profile: "polymux", Route: "polymux"}).withdraw()
	if calls := logged(t, dir); !slices.Contains(calls, "--profile polymux route remove polymux") {
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

func TestSettingsKeepValidNamesOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"profile":"Work Profile","route":"../bad","server":"https://tunnel.example","ca-cert":"/tmp/ca.pem"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readSettings(path)
	if got.Profile != "polymux" || got.Route != "polymux" || got.Server != "https://tunnel.example" || got.CA != "/tmp/ca.pem" {
		t.Fatalf("settings %+v", got)
	}
	if defaults := readSettings(filepath.Join(t.TempDir(), "absent")); defaults.Profile != "polymux" || defaults.Route != "polymux" {
		t.Fatalf("defaults %+v", defaults)
	}
}
