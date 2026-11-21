// polymux-context is the Context plugin's service. It tells agents what is open and available
// on this server's computer: the front app and window, apps, windows, browser tabs, native
// prompts, devices and the network. It only reads; nothing is launched or focused.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type toolCall struct {
	Args json.RawMessage `json:"args"`
}

type toolResult struct {
	Text string `json:"text"`
}

type service struct {
	read reader
	mac  bool
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := &service{read: reader{run: execRun, driver: driverPath()}, mac: runtime.GOOS == "darwin"}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := s.run(ctx, os.Getenv("POLYMUX_PLUGIN_PORT"), os.Getenv("POLYMUX_PLUGIN_TOKEN")); err != nil {
		log.Error("polymux-context stopped", "error", err)
		os.Exit(1)
	}
}

// driverPath finds cua-driver for the dialog check; without it that check reports unavailable.
func driverPath() string {
	if path, err := exec.LookPath("cua-driver"); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	for _, path := range []string{"/Applications/CuaDriver.app/Contents/MacOS/cua-driver", home + "/.local/bin/cua-driver"} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func (s *service) run(ctx context.Context, port, token string) error {
	if port == "" || token == "" {
		return errors.New("not started by Polymux")
	}
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: s.handler(token), ReadHeaderTimeout: 5 * time.Second}
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	return server.Shutdown(context.Background())
}

func (s *service) handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /tools/{name}", func(w http.ResponseWriter, r *http.Request) {
		var call toolCall
		if json.NewDecoder(r.Body).Decode(&call) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var args struct {
			App       string `json:"app"`
			Query     string `json:"query"`
			Installed bool   `json:"installed"`
		}
		_ = json.Unmarshal(call.Args, &args)
		text, ok := s.answer(r.Context(), r.PathValue("name"), args.App, args.Query, args.Installed)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toolResult{Text: text})
	})
	return authorised(token, mux)
}

func authorised(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		given, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *service) answer(ctx context.Context, tool, app, query string, all bool) (string, bool) {
	var out strings.Builder
	if !s.mac {
		switch tool {
		case "summary", "devices":
			fmt.Fprintf(&out, "This server runs on %s, which has no Mac desktop to read.\n", runtime.GOOS)
			showDevices(&out, s.read.devices(ctx, false))
			return out.String(), true
		case "apps", "windows", "tabs", "dialogs", "network":
			return fmt.Sprintf("This server runs on %s; only devices can be read here.", runtime.GOOS), true
		}
		return "", false
	}
	switch tool {
	case "summary":
		s.summary(ctx, &out)
	case "apps":
		if all {
			out.WriteString(strings.Join(installed(), "\n"))
			break
		}
		for _, a := range appRows(s.read.state(ctx)) {
			fmt.Fprintf(&out, "%s %s  %s  pid %d  %d windows\n", star(a.Front), a.Name, a.AppID, a.PID, a.Windows)
		}
	case "windows":
		showWindows(&out, windowRows(s.read.state(ctx), app))
	case "tabs":
		showTabs(&out, s.read.tabs(ctx, s.read.state(ctx), query), true)
	case "dialogs":
		showDialogs(&out, s.read.dialogs(ctx, app, nil, false))
	case "devices":
		showDevices(&out, s.read.devices(ctx, s.mac))
	case "network":
		showNetwork(&out, s.read.network(ctx, true))
	default:
		return "", false
	}
	if out.Len() == 0 {
		out.WriteString("Nothing to report.")
	}
	return out.String(), true
}

// summary is one reading of everything: where the user is, what's open, every tab, devices and network.
func (s *service) summary(ctx context.Context, out *strings.Builder) {
	st := s.read.state(ctx)
	apps, windows := appRows(st), windowRows(st, "")
	front, title := "?", ""
	for _, a := range apps {
		if a.Front {
			front = a.Name
		}
	}
	for _, w := range windows {
		if w.Front && w.Title != "" {
			title = " — " + w.Title
		}
	}
	fmt.Fprintf(out, "Front: %s%s\n", front, title)
	if idle := s.read.idleSeconds(ctx); idle != nil {
		fmt.Fprintf(out, "Input idle: %d s (recent input means someone is using this computer)\n", *idle)
	}
	showDialogs(out, s.read.dialogs(ctx, "", &st, true))
	if st.DefaultBrowser != "" {
		fmt.Fprintf(out, "Default browser: %s\n", st.DefaultBrowser)
	}
	names := make([]string, 0, len(apps))
	for _, a := range apps {
		names = append(names, fmt.Sprintf("%s(%d)", a.Name, a.Windows))
	}
	fmt.Fprintf(out, "Apps: %s\n\nWindows:\n", strings.Join(names, ", "))
	showWindows(out, windows)
	out.WriteString("\nTabs:\n")
	showTabs(out, s.read.tabs(ctx, st, ""), false)
	out.WriteString("\n")
	showDevices(out, s.read.devices(ctx, true))
	showNetwork(out, s.read.network(ctx, false))
	for _, warning := range st.Warnings {
		fmt.Fprintf(out, "note: %s\n", warning)
	}
}

func star(on bool) string {
	if on {
		return "*"
	}
	return " "
}

func showWindows(out *strings.Builder, rows []windowRow) {
	for _, w := range rows {
		hidden := ""
		if !w.Onscreen {
			hidden = "(hidden) "
		}
		fmt.Fprintf(out, "%s %s  pid %d  window %s  %s%s\n", star(w.Front), w.App, w.PID, w.WindowID, hidden, w.Title)
	}
}

func showTabs(out *strings.Builder, windows []tabWindow, ids bool) {
	if len(windows) == 0 {
		out.WriteString("No browser windows open.\n")
	}
	for _, w := range windows {
		if w.Warning != "" {
			fmt.Fprintf(out, "%s: tab inventory may be incomplete: %s\n", w.Browser, w.Warning)
			continue
		}
		if w.Unavailable {
			fmt.Fprintf(out, "%s: tabs unavailable (allow this app to control %s in System Settings → Privacy & Security → Automation)\n", w.Browser, w.Browser)
			continue
		}
		fmt.Fprintf(out, "%s window %s, %d tabs:\n", w.Browser, w.WindowID, len(w.Tabs))
		for _, t := range w.Tabs {
			id := ""
			if ids && t.TabID != "" {
				id = "tab " + t.TabID + "  "
			}
			fmt.Fprintf(out, "  %s %s%s  %s\n", star(t.Selected == true), id, truncate(clean(t.Title), 90), truncate(t.URL, 160))
		}
	}
}

func showDialogs(out *strings.Builder, r dialogReading) {
	for _, d := range r.Dialogs {
		app := d.App
		if app == "" {
			app = "?"
		}
		fmt.Fprintf(out, "Dialog: %s — %s (pid %d, window %s)\n", app, d.Kind, d.PID, d.WindowID)
	}
	switch {
	case r.Status == "unavailable":
		out.WriteString("Dialogs: unavailable (cua-driver could not read the desktop)\n")
	case r.Status == "unknown":
		fmt.Fprintf(out, "Dialog presence unknown: 0/%d windows checked\n", r.Eligible)
	case len(r.Dialogs) == 0:
		fmt.Fprintf(out, "Dialogs: none detected (%s; %d/%d windows checked; limited check)\n", r.Scope, r.Checked, r.Eligible)
	}
}

func showDevices(out *strings.Builder, d devices) {
	for _, p := range d.Tailscale {
		status := "offline"
		if p.Self {
			status = "this computer"
		} else if p.Online {
			status = "online"
		}
		fmt.Fprintf(out, "tailscale  %s  %s  %s  %s\n", p.Name, p.OS, p.IP, status)
	}
	for _, b := range d.Bluetooth {
		status := "paired, not in range"
		if b.Connected {
			status = "connected"
		} else if b.Nearby {
			status = "nearby rssi " + b.RSSI
		}
		fmt.Fprintf(out, "bluetooth  %s  %s  %s\n", b.Name, b.Type, status)
	}
	for _, x := range d.Displays {
		fmt.Fprintf(out, "display    %s  %s\n", x.Name, x.Resolution)
	}
}

func showNetwork(out *strings.Builder, n network) {
	ssid, ip, hotspot := n.SSID, n.IP, ""
	if ssid == "" {
		ssid = "Wi-Fi name not read"
	}
	if ip == "" {
		ip = "none"
	}
	if n.IPhoneHotspot {
		hotspot = "  (iPhone hotspot)"
	}
	fmt.Fprintf(out, "network    %s: %s  ip %s%s\n", n.Interface, ssid, ip, hotspot)
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
