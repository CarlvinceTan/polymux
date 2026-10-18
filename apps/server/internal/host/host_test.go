package host

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitHost(t *testing.T, c *Controller, condition func() bool) {
	t.Helper()
	end := time.Now().Add(4 * time.Second)
	for time.Now().Before(end) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("host state did not converge: %+v", c.Snapshot())
}

func TestHostSwitchPersistsAndStopsChild(t *testing.T) {
	t.Parallel()
	config := Config{Binary: "/bin/sh", Args: []string{"-c", "trap 'exit 0' TERM; while :; do sleep 0.1; done"}, Dir: t.TempDir(), Restart: 50 * time.Millisecond}
	c, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Enable(false); err != nil {
		t.Fatal(err)
	}
	c, err = Open(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); c.Run(ctx) }()
	defer func() { cancel(); <-done }()
	if c.Snapshot().Enabled || c.Snapshot().Running {
		t.Fatal("disabled service restarted on reopen")
	}
	if err := c.Enable(true); err != nil {
		t.Fatal(err)
	}
	waitHost(t, c, func() bool { return c.Snapshot().Running })
	pid := c.Snapshot().Pid
	if pid <= 0 {
		t.Fatal("no supervised process")
	}
	if err := c.Enable(false); err != nil {
		t.Fatal(err)
	}
	waitHost(t, c, func() bool { return !c.Snapshot().Running })
	reopened, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Enabled {
		t.Fatal("off state was not durable")
	}
}

func TestHostRestartsExitedProcess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "starts")
	c, err := Open(Config{Binary: "/bin/sh", Args: []string{"-c", `printf 'start\n' >> "$1"; sleep 0.1; exit 1`, "fixture", marker}, Dir: dir, Restart: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); c.Run(ctx) }()
	defer func() { cancel(); <-done }()
	waitHost(t, c, func() bool { data, _ := os.ReadFile(marker); return strings.Count(string(data), "start") >= 2 })
	if err := c.Enable(false); err != nil {
		t.Fatal(err)
	}
	waitHost(t, c, func() bool { return !c.Snapshot().Running })
	data, _ := os.ReadFile(marker)
	time.Sleep(150 * time.Millisecond)
	after, _ := os.ReadFile(marker)
	if string(after) != string(data) {
		t.Fatal("disabled server restarted")
	}
}

func TestHostAPIRequiresNativeValidPatch(t *testing.T) {
	t.Parallel()
	c, err := Open(Config{Binary: "/bin/sh", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body, origin string
		status             int
	}{
		{"off", `{"enabled":false}`, "", 200}, {"missing", `{}`, "", 400}, {"unknown", `{"enabled":true,"command":"bad"}`, "", 400},
		{"browser", `{"enabled":true}`, "https://untrusted.test", 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPatch, "/host", strings.NewReader(test.body))
			r.Header.Set("Origin", test.origin)
			w := httptest.NewRecorder()
			c.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status %d", w.Code)
			}
		})
	}
	if c.Snapshot().Enabled {
		t.Fatal("invalid request enabled server")
	}
	w := httptest.NewRecorder()
	c.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/host", nil))
	var response map[string]any
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || response["enabled"] != false {
		t.Fatal("host status unavailable while server off")
	}
}

func TestHostUnitHasNoShellOrCredentialEnvironment(t *testing.T) {
	t.Parallel()
	data, err := Unit("/Users/user/a & b/server", "/Users/user/data", "100.64.0.1:7337", "100.64.0.1:7338")
	if err != nil {
		t.Fatal(err)
	}
	var document struct{ XMLName xml.Name }
	if xml.Unmarshal(data, &document) != nil || document.XMLName.Local != "plist" {
		t.Fatal("invalid plist")
	}
	if strings.Contains(string(data), "EnvironmentVariables") || strings.Contains(string(data), "/bin/sh") || !strings.Contains(string(data), "a &amp; b") {
		t.Fatal("unsafe service unit")
	}
}
