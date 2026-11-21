package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const fakeState = `{"active":{"app_id":"net.imput.helium","pid":10,"window_id":"200"},"default_browser":"net.imput.helium",
"apps":{"a":{"name":"Helium","app_id":"net.imput.helium","pid":10},"b":{"name":"Notes","app_id":"com.apple.Notes","pid":20}},
"surfaces":[{"window_id":"200","title":"Docs\u200b","onscreen":true,"app_id":"net.imput.helium","pid":10},
{"window_id":"300","title":"Shopping","onscreen":false,"app_id":"com.apple.Notes","pid":20}],"_warnings":["front window frame matched several windows"]}`

const fakeTabs = `{"windows":[{"window_id":"1","tabs":[{"title":"Polymux docs","url":"https://polymux.com/docs","tab_id":"5","selected":true},
{"title":"Mail","url":"https://mail.example.com","tab_id":"6","selected":false}]}]}`

const fakeWindows = `{"windows":[{"app_name":"Helium","pid":10,"window_id":200,"is_on_screen":true,"layer":0,"z_index":5},
{"app_name":"SecurityAgent","pid":30,"window_id":400,"is_on_screen":true,"layer":0,"z_index":9}]}`

func fake(_ context.Context, _ time.Duration, name string, args ...string) string {
	switch {
	case name == "osascript" && len(args) == 4:
		return fakeState
	case name == "osascript":
		return fakeTabs
	case name == "ioreg":
		return `    | |   "HIDIdleTime" = 42000000000`
	case name == "route":
		return "   route to: default\n  interface: en1\n"
	case name == "ipconfig":
		return "172.20.10.4\n"
	case name == "cua" && args[1] == "list_windows":
		return fakeWindows
	case name == "cua" && args[1] == "verify_state" && strings.Contains(args[2], `"pid":10`):
		return `{"predicates":[{"index":0,"status":"satisfied"},{"index":1,"status":"unsatisfied","observed_json":"[]"}]}`
	case name == "cua":
		return `{"predicates":[{"index":0,"status":"unsatisfied","observed_json":"[]"}]}`
	case strings.HasSuffix(name, "ailscale"):
		return `{"Self":{"HostName":"mini","OS":"macOS","TailscaleIPs":["100.1.1.1"]},"Peer":{"k":{"HostName":"phone","OS":"iOS","Online":true,"TailscaleIPs":["100.2.2.2"]}}}`
	case name == "system_profiler":
		return `{"SPBluetoothDataType":[{"device_connected":[{"AirPods":{"device_minorType":"Headphones","device_rssi":"-50"}}]}],
"SPDisplaysDataType":[{"spdisplays_ndrvs":[{"_name":"Studio Display","_spdisplays_resolution":"5120 x 2880"}]}]}`
	}
	return ""
}

func ask(t *testing.T, s *service, tool, args string) string {
	t.Helper()
	request := httptest.NewRequest("POST", "/tools/"+tool, bytes.NewBufferString(`{"chat":"c","args":`+args+`}`))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	s.handler("secret").ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("%s: status %d", tool, response.Code)
	}
	var result toolResult
	_ = json.Unmarshal(response.Body.Bytes(), &result)
	return result.Text
}

// The summary holds every part of a reading: front window, idle time, prompts, apps, windows, every tab, devices and network.
func TestSummaryReadsEverything(t *testing.T) {
	s := &service{read: reader{run: fake, driver: "cua"}, mac: true}
	text := ask(t, s, "summary", `{}`)
	for _, want := range []string{
		"Front: Helium — Docs\n", "Input idle: 42 s", "Dialog: Helium — AXSheet (pid 10, window 200)",
		"Dialog: SecurityAgent — possible system prompt", "Apps: Helium(1), Notes(1)",
		"* Helium  pid 10  window 200  Docs", "  Notes  pid 20  window 300  (hidden) Shopping",
		"Helium window 1, 2 tabs:", "* Polymux docs  https://polymux.com/docs", "  Mail  https://mail.example.com",
		"tailscale  phone  iOS  100.2.2.2  online", "bluetooth  AirPods  Headphones  connected",
		"display    Studio Display  5120 x 2880", "network    en1: Wi-Fi name not read  ip 172.20.10.4  (iPhone hotspot)",
		"note: front window frame matched several windows",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("summary is missing %q:\n%s", want, text)
		}
	}
}

func TestFilters(t *testing.T) {
	s := &service{read: reader{run: fake, driver: "cua"}, mac: true}
	if text := ask(t, s, "tabs", `{"query":"mail"}`); !strings.Contains(text, "tab 6  Mail") || strings.Contains(text, "Polymux") {
		t.Errorf("tabs %q", text)
	}
	if text := ask(t, s, "windows", `{"app":"notes"}`); !strings.Contains(text, "Shopping") || strings.Contains(text, "Docs") {
		t.Errorf("windows %q", text)
	}
	if text := ask(t, s, "apps", `{}`); !strings.HasPrefix(text, "* Helium") {
		t.Errorf("apps %q", text)
	}
}

func TestWithoutDriverOrMac(t *testing.T) {
	s := &service{read: reader{run: fake}, mac: true}
	if text := ask(t, s, "dialogs", `{}`); !strings.Contains(text, "unavailable") {
		t.Errorf("dialogs %q", text)
	}
	linux := &service{read: reader{run: fake}, mac: false}
	if text := ask(t, linux, "summary", `{}`); !strings.Contains(text, "no Mac desktop") || !strings.Contains(text, "phone") {
		t.Errorf("linux summary %q", text)
	}
	if text := ask(t, linux, "tabs", `{}`); !strings.Contains(text, "only devices") {
		t.Errorf("linux tabs %q", text)
	}
}

func TestRefusesWithoutToken(t *testing.T) {
	response := httptest.NewRecorder()
	(&service{read: reader{run: fake}}).handler("secret").ServeHTTP(response, httptest.NewRequest("POST", "/tools/summary", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", response.Code)
	}
}
