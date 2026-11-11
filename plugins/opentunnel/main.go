// polymux-opentunnel publishes this server's loopback API through an OpenTunnel
// route, so a paired device reaches it from any network without an account.
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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	refresh     = 30 * time.Second
	commandWait = 20 * time.Second
	routeWait   = 6 * time.Minute
)

type settings struct {
	Profile string `json:"profile"`
	Route   string `json:"route"`
	Server  string `json:"server"`
	CA      string `json:"ca-cert"`
}

// report is what GET /transport answers. OpenTunnel terminates TLS itself, so
// it can't publish the pinned device listener and leaves that field empty.
type report struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
	API     string `json:"api,omitempty"`
}

type service struct {
	cli     string
	api     string
	options settings
	log     *slog.Logger
	find    func() string

	mu      sync.Mutex
	current report
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := &service{
		cli:     cliPath(),
		api:     os.Getenv("POLYMUX_PUBLISH_API"),
		options: readSettings(os.Getenv("POLYMUX_PLUGIN_SETTINGS")),
		log:     log,
		current: report{State: "starting"},
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := s.run(ctx, os.Getenv("POLYMUX_PLUGIN_PORT"), os.Getenv("POLYMUX_PLUGIN_TOKEN")); err != nil {
		log.Error("polymux-opentunnel stopped", "error", err)
		os.Exit(1)
	}
}

// cliPath finds the opentunnel command: an override, the PATH, then the
// places its installer and common package managers put it.
func cliPath() string {
	if path := os.Getenv("POLYMUX_OPENTUNNEL_CLI"); path != "" {
		return path
	}
	if path, err := exec.LookPath("opentunnel"); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	for _, path := range []string{
		filepath.Join(home, ".local", "bin", "opentunnel"),
		"/opt/homebrew/bin/opentunnel",
		"/usr/local/bin/opentunnel",
	} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func readSettings(path string) settings {
	values := settings{Profile: "polymux", Route: "polymux"}
	var saved settings
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &saved) == nil {
		if profileName.MatchString(saved.Profile) {
			values.Profile = saved.Profile
		}
		if routeName.MatchString(saved.Route) {
			values.Route = saved.Route
		}
		values.Server = strings.TrimSpace(saved.Server)
		values.CA = strings.TrimSpace(saved.CA)
	}
	return values
}

func (s *service) run(ctx context.Context, port, token string) error {
	if port == "" || token == "" {
		return errors.New("not started by Polymux")
	}
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: s.handler(token), ReadHeaderTimeout: 5 * time.Second}
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	done := make(chan struct{})
	go func() { defer close(done); s.loop(ctx) }()
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	<-done
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

// reconcile points a route at this server's API and reports where it landed.
func (s *service) reconcile(ctx context.Context) report {
	cli := s.command()
	if cli == "" {
		return report{State: "error", Message: "OpenTunnel isn't installed."}
	}
	args := []string{"--profile", s.options.Profile}
	if s.options.Server != "" {
		args = append(args, "--api", s.options.Server)
	}
	if s.options.CA != "" {
		args = append(args, "--ca-cert", s.options.CA)
	}
	args = append(args, "route", "add", s.api, "--name", s.options.Route)
	out, err := s.runCommand(ctx, cli, routeWait, args...)
	if err != nil {
		return report{State: "error", Message: reason(out, err)}
	}
	if url := lastURL(out); url != "" {
		return report{State: "ready", API: url}
	}
	return report{State: "starting", Message: "Waiting for the route."}
}

// withdraw leaves the user's other routes alone by removing only this route.
func (s *service) withdraw() {
	cli := s.command()
	if cli == "" {
		return
	}
	out, err := s.runCommand(context.Background(), cli, commandWait,
		"--profile", s.options.Profile, "route", "remove", s.options.Route)
	if err != nil {
		s.log.Warn("remove route", "error", reason(out, err))
	}
}

// command resolves the CLI again, so installing OpenTunnel after the plugin starts works.
func (s *service) command() string {
	if s.cli != "" {
		return s.cli
	}
	if s.find != nil {
		return s.find()
	}
	return cliPath()
}

func (s *service) runCommand(ctx context.Context, cli string, wait time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	out, err := exec.CommandContext(ctx, cli, args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded && err != nil {
		return out, fmt.Errorf("OpenTunnel didn't answer in %s", wait)
	}
	return out, err
}

// lastURL is the route URL that `route add` prints once it has one.
func lastURL(out []byte) string {
	fields := strings.Fields(string(out))
	for i := len(fields) - 1; i >= 0; i-- {
		value := strings.TrimRight(fields[i], ".,;)")
		if strings.HasPrefix(value, "https://") && len(value) > len("https://") {
			return value
		}
	}
	return ""
}

// reason is the last line a failed command printed, without its prefix.
func reason(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "Error: ")); line != "" {
			return fmt.Sprintf("%.200s", line)
		}
	}
	return err.Error()
}

var profileName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

var routeName = regexp.MustCompile(`^(?:@|[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)$`)
