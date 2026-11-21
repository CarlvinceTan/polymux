package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Ported from cstack's `context` tool: everything here only reads. Nothing is launched,
// activated or focused: windows come from CoreGraphics, apps from AppKit, tabs from each
// browser's scripting dictionary (only browsers already running are asked).

//go:embed state.jxa.js
var stateScript string

//go:embed tabs.jxa.js
var tabsScript string

var (
	chromium = []string{"net.imput.helium", "com.google.chrome", "com.google.chrome.canary", "com.brave.browser",
		"com.microsoft.edgemac", "com.vivaldi.vivaldi", "company.thebrowser.browser", "org.chromium.chromium"}
	safari       = []string{"com.apple.safari", "com.apple.safaritechnologypreview"}
	promptOwners = []string{"securityagent", "usernotificationcenter", "coreservicesuiagent",
		"universalaccessauthwarn", "openandsavepanelservice", "coreauthd",
		"localauthenticationremoteservice", "screencapturekit picker"}
	dialogRoles = []string{"AXSheet", "AXDialog", "AXSystemDialog"}
)

// reader runs the commands a reading needs; tests swap it.
type reader struct {
	run    func(ctx context.Context, timeout time.Duration, name string, args ...string) string
	driver string
}

func execRun(ctx context.Context, timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil
	out, _ := cmd.Output()
	return string(out)
}

func (r reader) jxa(ctx context.Context, script string, timeout time.Duration, args ...string) map[string]any {
	out := r.run(ctx, timeout, "osascript", append([]string{"-l", "JavaScript", "-e", script}, args...)...)
	var value map[string]any
	if json.Unmarshal([]byte(out), &value) != nil {
		return map[string]any{}
	}
	return value
}

type app struct {
	Name  string `json:"name"`
	AppID string `json:"app_id"`
	PID   int    `json:"pid"`
}

type surface struct {
	WindowID string `json:"window_id"`
	Title    string `json:"title"`
	Onscreen bool   `json:"onscreen"`
	AppID    string `json:"app_id"`
	PID      int    `json:"pid"`
}

type state struct {
	Active struct {
		AppID    string `json:"app_id"`
		PID      int    `json:"pid"`
		WindowID string `json:"window_id"`
	} `json:"active"`
	DefaultBrowser string         `json:"default_browser"`
	Apps           map[string]app `json:"apps"`
	Surfaces       []surface      `json:"surfaces"`
	Warnings       []string       `json:"_warnings"`
}

func (r reader) state(ctx context.Context) state {
	var s state
	out := r.run(ctx, 20*time.Second, "osascript", "-l", "JavaScript", "-e", stateScript)
	_ = json.Unmarshal([]byte(out), &s)
	return s
}

type tab struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	TabID    string `json:"tab_id,omitempty"`
	Selected any    `json:"selected,omitempty"`
}

type tabWindow struct {
	Browser     string `json:"browser"`
	WindowID    string `json:"window_id"`
	Tabs        []tab  `json:"tabs"`
	Unavailable bool   `json:"unavailable,omitempty"`
}

func (r reader) tabs(ctx context.Context, s state, query string) []tabWindow {
	out := []tabWindow{}
	for _, a := range sortedApps(s) {
		id := strings.ToLower(a.AppID)
		schema := ""
		if slices.Contains(chromium, id) {
			schema = "chromium"
		} else if slices.Contains(safari, id) {
			schema = "safari"
		}
		if schema == "" {
			continue
		}
		raw := r.run(ctx, 20*time.Second, "osascript", "-l", "JavaScript", "-e", tabsScript, a.AppID, schema)
		var read struct {
			Windows []struct {
				WindowID string `json:"window_id"`
				Tabs     []tab  `json:"tabs"`
			} `json:"windows"`
		}
		if json.Unmarshal([]byte(raw), &read) != nil {
			out = append(out, tabWindow{Browser: a.Name, Tabs: []tab{}, Unavailable: true})
			continue
		}
		for _, w := range read.Windows {
			rows := []tab{}
			for _, t := range w.Tabs {
				if query == "" || strings.Contains(strings.ToLower(t.Title+" "+t.URL), strings.ToLower(query)) {
					rows = append(rows, t)
				}
			}
			if len(rows) > 0 || query == "" {
				out = append(out, tabWindow{Browser: a.Name, WindowID: w.WindowID, Tabs: rows})
			}
		}
	}
	return out
}

func clean(title string) string {
	return strings.TrimSpace(strings.Map(func(ch rune) rune {
		if !unicode.IsPrint(ch) && ch != ' ' || ch >= 0x200b && ch <= 0x206f {
			return -1
		}
		return ch
	}, title))
}

type peer struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Online bool   `json:"online"`
	Self   bool   `json:"self,omitempty"`
	IP     string `json:"ip"`
}

type bluetooth struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Type      string `json:"type"`
	Nearby    bool   `json:"nearby"`
	RSSI      string `json:"rssi"`
	Battery   string `json:"battery"`
}

type display struct {
	Name       string `json:"name"`
	Resolution string `json:"resolution"`
}

type devices struct {
	Tailscale []peer      `json:"tailscale"`
	Bluetooth []bluetooth `json:"bluetooth"`
	Displays  []display   `json:"displays"`
}

func tailscaleCLI() string {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path
	}
	return "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
}

func (r reader) devices(ctx context.Context, mac bool) devices {
	d := devices{Tailscale: []peer{}, Bluetooth: []bluetooth{}, Displays: []display{}}
	var ts struct {
		Self *struct {
			HostName     string
			OS           string
			TailscaleIPs []string
		}
		Peer map[string]struct {
			HostName     string
			OS           string
			Online       bool
			TailscaleIPs []string
		}
	}
	if json.Unmarshal([]byte(r.run(ctx, 20*time.Second, tailscaleCLI(), "status", "--json")), &ts) == nil {
		if ts.Self != nil {
			d.Tailscale = append(d.Tailscale, peer{Name: ts.Self.HostName, OS: ts.Self.OS, Online: true, Self: true, IP: first(ts.Self.TailscaleIPs)})
		}
		names := make([]string, 0, len(ts.Peer))
		for key := range ts.Peer {
			names = append(names, key)
		}
		slices.Sort(names)
		for _, key := range names {
			p := ts.Peer[key]
			d.Tailscale = append(d.Tailscale, peer{Name: p.HostName, OS: p.OS, Online: p.Online, IP: first(p.TailscaleIPs)})
		}
	}
	if !mac {
		return d
	}
	var sp struct {
		Bluetooth []map[string]json.RawMessage `json:"SPBluetoothDataType"`
		Displays  []struct {
			Displays []struct {
				Name       string `json:"_name"`
				Resolution string `json:"_spdisplays_resolution"`
			} `json:"spdisplays_ndrvs"`
		} `json:"SPDisplaysDataType"`
	}
	if json.Unmarshal([]byte(r.run(ctx, 30*time.Second, "system_profiler", "-json", "SPBluetoothDataType", "SPDisplaysDataType")), &sp) == nil {
		for _, controller := range sp.Bluetooth {
			for _, group := range []struct {
				key       string
				connected bool
			}{{"device_connected", true}, {"device_not_connected", false}} {
				var entries []map[string]map[string]any
				_ = json.Unmarshal(controller[group.key], &entries)
				for _, entry := range entries {
					for name, v := range entry {
						rssi := text(v["device_rssi"])
						d.Bluetooth = append(d.Bluetooth, bluetooth{Name: name, Connected: group.connected, Type: text(v["device_minorType"]),
							Nearby: rssi != "", RSSI: rssi, Battery: text(v["device_batteryLevelMain"])})
					}
				}
			}
		}
		for _, gpu := range sp.Displays {
			for _, x := range gpu.Displays {
				d.Displays = append(d.Displays, display{Name: x.Name, Resolution: x.Resolution})
			}
		}
	}
	return d
}

type network struct {
	Interface     string `json:"interface"`
	IP            string `json:"ip"`
	IPhoneHotspot bool   `json:"iphone_hotspot"`
	SSID          string `json:"ssid"`
}

func (r reader) network(ctx context.Context, full bool) network {
	iface := "en0"
	for _, line := range strings.Split(r.run(ctx, 20*time.Second, "route", "-n", "get", "default"), "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "interface:"); ok {
			iface = strings.TrimSpace(name)
			break
		}
	}
	ip := strings.TrimSpace(r.run(ctx, 20*time.Second, "ipconfig", "getifaddr", iface))
	n := network{Interface: iface, IP: ip, IPhoneHotspot: strings.HasPrefix(ip, "172.20.10.")}
	if full {
		var sp struct {
			AirPort []struct {
				Interfaces []struct {
					Current json.RawMessage `json:"spairport_current_network_information"`
				} `json:"spairport_airport_interfaces"`
			} `json:"SPAirPortDataType"`
		}
		if json.Unmarshal([]byte(r.run(ctx, 30*time.Second, "system_profiler", "-json", "SPAirPortDataType")), &sp) == nil && len(sp.AirPort) > 0 {
			for _, i := range sp.AirPort[0].Interfaces {
				var current struct {
					Name string `json:"_name"`
				}
				if json.Unmarshal(i.Current, &current) == nil && current.Name != "" {
					n.SSID = current.Name
					break
				}
			}
		}
	}
	return n
}

func installed() []string {
	home, _ := os.UserHomeDir()
	seen := map[string]bool{}
	for _, dir := range []string{"/Applications", "/System/Applications", "/System/Applications/Utilities", filepath.Join(home, "Applications")} {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if name, ok := strings.CutSuffix(entry.Name(), ".app"); ok {
				seen[name] = true
			}
		}
	}
	apps := make([]string, 0, len(seen))
	for name := range seen {
		apps = append(apps, name)
	}
	slices.SortFunc(apps, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return apps
}

type appRow struct {
	app
	Windows int  `json:"windows"`
	Front   bool `json:"front"`
}

func sortedApps(s state) []app {
	apps := make([]app, 0, len(s.Apps))
	for _, a := range s.Apps {
		apps = append(apps, a)
	}
	slices.SortFunc(apps, func(a, b app) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return apps
}

func appRows(s state) []appRow {
	count := map[int]int{}
	for _, w := range s.Surfaces {
		count[w.PID]++
	}
	rows := []appRow{}
	for _, a := range sortedApps(s) {
		rows = append(rows, appRow{app: a, Windows: count[a.PID], Front: a.PID == s.Active.PID})
	}
	slices.SortStableFunc(rows, func(a, b appRow) int {
		if a.Front != b.Front {
			if a.Front {
				return -1
			}
			return 1
		}
		return 0
	})
	return rows
}

type windowRow struct {
	App      string `json:"app"`
	PID      int    `json:"pid"`
	WindowID string `json:"window_id"`
	Title    string `json:"title"`
	Onscreen bool   `json:"onscreen"`
	Front    bool   `json:"front"`
}

func windowRows(s state, filter string) []windowRow {
	names := map[int]string{}
	for _, a := range s.Apps {
		names[a.PID] = a.Name
	}
	rows := []windowRow{}
	for _, w := range s.Surfaces {
		if filter != "" && !strings.Contains(strings.ToLower(names[w.PID]+" "+w.AppID), strings.ToLower(filter)) {
			continue
		}
		name := names[w.PID]
		if name == "" {
			name = w.AppID
		}
		rows = append(rows, windowRow{App: name, PID: w.PID, WindowID: w.WindowID, Title: clean(w.Title), Onscreen: w.Onscreen, Front: w.WindowID == s.Active.WindowID})
	}
	return rows
}

type dialog struct {
	App      string `json:"app"`
	PID      int    `json:"pid"`
	WindowID string `json:"window_id"`
	Kind     string `json:"kind"`
}

type dialogReading struct {
	Scope    string   `json:"scope"`
	Checked  int      `json:"windows_checked"`
	Eligible int      `json:"windows_eligible"`
	Status   string   `json:"status"`
	Dialogs  []dialog `json:"dialogs"`
}

func (r reader) driverCall(ctx context.Context, tool string, params any, deadline time.Time) map[string]any {
	remaining := time.Until(deadline)
	if remaining <= 0 || r.driver == "" {
		return map[string]any{}
	}
	data, _ := json.Marshal(params)
	var value map[string]any
	if json.Unmarshal([]byte(r.run(ctx, min(2*time.Second, remaining), r.driver, "call", tool, string(data))), &value) != nil {
		return map[string]any{}
	}
	return value
}

type driverWindow struct {
	AppName  string `json:"app_name"`
	PID      int    `json:"pid"`
	WindowID int    `json:"window_id"`
	OnScreen bool   `json:"is_on_screen"`
	Layer    int    `json:"layer"`
	Z        int    `json:"z_index"`
}

// dialogs reports native sheets and prompts through cua-driver. It only says one is there:
// never its text, controls or a screenshot.
func (r reader) dialogs(ctx context.Context, filter string, front *state, summary bool) dialogReading {
	deadline := time.Now().Add(3 * time.Second)
	scope := filter
	if scope == "" {
		scope = map[bool]string{true: "front app", false: "visible windows"}[summary]
	}
	reading := dialogReading{Scope: scope, Status: "unavailable", Dialogs: []dialog{}}
	listed, ok := r.driverCall(ctx, "list_windows", map[string]any{}, deadline)["windows"]
	if !ok {
		return reading
	}
	var windows []driverWindow
	data, _ := json.Marshal(listed)
	if json.Unmarshal(data, &windows) != nil {
		return reading
	}
	visible := []driverWindow{}
	for _, w := range windows {
		if w.OnScreen && w.Layer == 0 && w.PID != 0 && w.WindowID != 0 &&
			(filter == "" || strings.Contains(strings.ToLower(w.AppName), strings.ToLower(filter))) {
			visible = append(visible, w)
		}
	}
	slices.SortStableFunc(visible, func(a, b driverWindow) int { return b.Z - a.Z })
	reading.Status = "limited"
	for _, w := range visible {
		if slices.Contains(promptOwners, strings.ToLower(w.AppName)) {
			reading.Dialogs = append(reading.Dialogs, dialog{App: w.AppName, PID: w.PID, WindowID: strconv.Itoa(w.WindowID), Kind: "possible system prompt"})
		}
	}
	if front != nil {
		rank := func(w driverWindow) int {
			n := 0
			if w.PID != front.Active.PID {
				n += 2
			}
			if strconv.Itoa(w.WindowID) != front.Active.WindowID {
				n++
			}
			return n
		}
		slices.SortStableFunc(visible, func(a, b driverWindow) int { return rank(a) - rank(b) })
	}
	if summary && len(visible) > 0 {
		pid := visible[0].PID
		visible = slices.DeleteFunc(visible, func(w driverWindow) bool { return w.PID != pid })
	}
	reading.Eligible = len(visible)
	confirmed := map[string]bool{}
	limit := map[bool]int{true: 2, false: 4}[summary]
	expect := []map[string]any{}
	for _, role := range dialogRoles {
		expect = append(expect, map[string]any{"element": map[string]any{"selector": map[string]any{"role": role}}})
	}
	for _, w := range visible[:min(limit, len(visible))] {
		raw, ok := r.driverCall(ctx, "verify_state", map[string]any{"pid": w.PID, "window_id": w.WindowID, "include_screenshot": false,
			"timeout_ms": 0, "stable_samples": 1, "expect": expect}, deadline)["predicates"]
		if !ok {
			continue
		}
		var predicates []struct {
			Status   string `json:"status"`
			Index    *int   `json:"index"`
			Observed any    `json:"observed_json"`
		}
		data, _ := json.Marshal(raw)
		if json.Unmarshal(data, &predicates) != nil {
			continue
		}
		for _, p := range predicates {
			if p.Status == "satisfied" || p.Observed != nil && p.Observed != "" {
				reading.Checked++
				break
			}
		}
		for _, p := range predicates {
			if p.Status != "satisfied" || p.Index == nil || *p.Index < 0 || *p.Index >= len(dialogRoles) {
				continue
			}
			id := strconv.Itoa(w.WindowID)
			confirmed[id] = true
			reading.Dialogs = append(reading.Dialogs, dialog{App: w.AppName, PID: w.PID, WindowID: id, Kind: dialogRoles[*p.Index]})
		}
	}
	reading.Dialogs = slices.DeleteFunc(reading.Dialogs, func(d dialog) bool { return d.Kind == "possible system prompt" && confirmed[d.WindowID] })
	if reading.Eligible > 0 && reading.Checked == 0 {
		reading.Status = "unknown"
	}
	return reading
}

// idleSeconds is the time since the last keyboard or mouse input: whether someone is at this Mac.
func (r reader) idleSeconds(ctx context.Context) *int {
	for _, line := range strings.Split(r.run(ctx, 10*time.Second, "ioreg", "-c", "IOHIDSystem"), "\n") {
		if strings.Contains(line, "HIDIdleTime") {
			fields := strings.Fields(line)
			if n, err := strconv.ParseInt(fields[len(fields)-1], 10, 64); err == nil {
				seconds := int(n / 1_000_000_000)
				return &seconds
			}
			return nil
		}
	}
	return nil
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func text(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}
