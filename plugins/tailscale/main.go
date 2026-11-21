// polymux-tailscale is the Tailscale transport plugin's service. It publishes the server's
// listeners on the tailnet with `tailscale serve` and reports the resulting addresses.
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	refresh     = 30 * time.Second
	commandWait = 20 * time.Second
	macCLI      = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
)

type settings struct {
	HTTPSPort  string `json:"https-port"`
	DevicePort string `json:"device-port"`
}

// report is what GET /transport answers.
type report struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
	API     string `json:"api,omitempty"`
	Device  string `json:"device,omitempty"`
}

type service struct {
	cli     string
	api     string
	device  string
	options settings
	log     *slog.Logger

	mu      sync.Mutex
	current report
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := &service{
		cli: cliPath(), api: os.Getenv("POLYMUX_PUBLISH_API"), device: os.Getenv("POLYMUX_PUBLISH_DEVICE"),
		options: readSettings(os.Getenv("POLYMUX_PLUGIN_SETTINGS")), log: log,
		current: report{State: "starting"},
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := s.run(ctx, os.Getenv("POLYMUX_PLUGIN_PORT"), os.Getenv("POLYMUX_PLUGIN_TOKEN")); err != nil {
		log.Error("polymux-tailscale stopped", "error", err)
		os.Exit(1)
	}
}

// cliPath finds the tailscale command: an override, the PATH, then the Mac app's own.
func cliPath() string {
	if path := os.Getenv("POLYMUX_TAILSCALE_CLI"); path != "" {
		return path
	}
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path
	}
	return macCLI
}

func readSettings(path string) settings {
	values := settings{HTTPSPort: "8443", DevicePort: "7339"}
	var saved settings
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &saved) == nil {
		if validPort(saved.HTTPSPort) {
			values.HTTPSPort = saved.HTTPSPort
		}
		if validPort(saved.DevicePort) {
			values.DevicePort = saved.DevicePort
		}
	}
	return values
}

func validPort(value string) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n > 0 && n < 65536 && strconv.Itoa(n) == value
}

func (s *service) run(ctx context.Context, port, token string) error {
	if port == "" || token == "" {
		return errors.New("not started by Polymux")
	}
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: s.handler(token), ReadHeaderTimeout: 5 * time.Second}
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	go s.loop(ctx)
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	s.withdraw()
	return server.Shutdown(context.Background())
}

func (s *service) handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /transport", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		current := s.current
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(current)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		given, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *service) loop(ctx context.Context) {
	for {
		next := s.reconcile(ctx)
		s.mu.Lock()
		s.current = next
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(refresh):
		}
	}
}

// reconcile publishes both listeners and reports where they can be reached.
func (s *service) reconcile(ctx context.Context) report {
	name, err := s.dnsName(ctx)
	if err != nil {
		return report{State: "error", Message: err.Error()}
	}
	for _, args := range [][]string{
		{"serve", "--bg", "--https=" + s.options.HTTPSPort, "http://" + s.api},
		{"serve", "--bg", "--tcp=" + s.options.DevicePort, "tcp://" + s.device},
	} {
		if out, err := s.tailscale(ctx, args...); err != nil {
			return report{State: "error", Message: firstLine(out, err)}
		}
	}
	return report{State: "ready", API: origin(name, s.options.HTTPSPort), Device: origin(name, s.options.DevicePort)}
}

// withdraw takes both listeners off the tailnet when the plugin stops.
func (s *service) withdraw() {
	ctx, cancel := context.WithTimeout(context.Background(), commandWait)
	defer cancel()
	for _, args := range [][]string{{"serve", "--https=" + s.options.HTTPSPort, "off"}, {"serve", "--tcp=" + s.options.DevicePort, "off"}} {
		if _, err := s.tailscale(ctx, args...); err != nil {
			s.log.Warn("withdraw listener", "args", args, "error", err)
		}
	}
}

func (s *service) dnsName(ctx context.Context) (string, error) {
	out, err := s.tailscale(ctx, "status", "--json")
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return "", errors.New("Tailscale isn't installed.")
	}
	var status struct {
		BackendState string
		Self         struct{ DNSName string }
	}
	if json.Unmarshal(out, &status) != nil {
		return "", errors.New("Tailscale isn't running.")
	}
	name := strings.TrimSuffix(status.Self.DNSName, ".")
	switch {
	case status.BackendState == "NeedsLogin":
		return "", errors.New("Sign in to Tailscale.")
	case status.BackendState != "Running":
		return "", errors.New("Tailscale isn't running.")
	case name == "":
		return "", errors.New("Turn on MagicDNS in your Tailscale settings.")
	}
	return name, nil
}

func (s *service) tailscale(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandWait)
	defer cancel()
	return exec.CommandContext(ctx, s.cli, args...).CombinedOutput()
}

// origin is an HTTPS address on the tailnet; 443 is left implicit.
func origin(name, port string) string {
	if port == "443" {
		return "https://" + name
	}
	return "https://" + name + ":" + port
}

// firstLine is the first line of a failed command's output, which names the problem.
func firstLine(out []byte, err error) string {
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return fmt.Sprintf("%.200s", line)
		}
	}
	return err.Error()
}
