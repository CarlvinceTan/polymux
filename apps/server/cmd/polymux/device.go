package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
)

func runDevice(args []string) error {
	if len(args) == 0 || args[0] != "pair" && args[0] != "list" && args[0] != "revoke" {
		return errors.New("usage: polymux-server device pair|list|revoke [-data path] [-url https://IP:7339] [-agent ID] [-id ID] [-tailscale] [-remote URL]")
	}
	flags := flag.NewFlagSet("device", flag.ContinueOnError)
	dir := flags.String("data", os.Getenv("POLYMUX_DATA_DIR"), "server data directory")
	address := flags.String("url", "", "pinned TLS device listener")
	agent := flags.String("agent", "", "agent the linked app works with")
	id := flags.String("id", "", "device to revoke")
	tailscale := flags.Bool("tailscale", os.Getenv("POLYMUX_TAILSCALE") == "1", "use a Tailscale device URL (requires Tailscale)")
	remote := flags.String("remote", "", "user-entered pairing address")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*dir = filepath.Join(home, "Library", "Application Support", "Polymux", "server")
	}
	ip := ""
	if *tailscale {
		var err error
		ip, err = tailnet()
		if err != nil {
			return err
		}
	}
	if *address == "" {
		if ip == "" {
			ip = lanHost()
		}
		*address = "https://" + net.JoinHostPort(ip, "7339")
	}
	u, err := url.Parse(*address)
	if err != nil || u.Hostname() == "" {
		return errors.New("invalid device URL")
	}
	ctx := context.Background()
	s, err := device.Open(ctx, *dir, identityHost(u.Hostname()))
	if err != nil {
		return err
	}
	defer s.Close()
	switch args[0] {
	case "pair":
		if *agent == "" {
			return errors.New("choose an existing agent with -agent")
		}
		db, err := sql.Open("sqlite3", "file:"+filepath.Join(*dir, "polymux.db")+"?mode=ro")
		if err != nil {
			return err
		}
		defer db.Close()
		var found string
		if err := db.QueryRowContext(ctx, `SELECT id FROM agents WHERE id=?`, *agent).Scan(&found); err != nil {
			return errors.New("agent not found; start the server and create it first")
		}
		port := u.Port()
		if port == "" {
			port = "7339"
		}
		entered := []string{}
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "url" {
				entered = append(entered, *address)
			}
		})
		if *remote != "" {
			entered = append(entered, *remote)
		}
		if v2, err := s.IssueApp(ctx, pairCandidates(ctx, port, entered), *agent); err == nil {
			fmt.Println(v2)
		} else if !errors.Is(err, device.ErrNoAddress) {
			return err
		}
		fmt.Fprintln(os.Stderr, "Expires in 3 minutes, single use. Paste it into Polymux Settings. Treat it as a temporary credential.")
	case "list":
		pairs, err := s.List(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(pairs)
	case "revoke":
		if *id == "" {
			return errors.New("choose a device with -id")
		}
		if err := s.Revoke(ctx, *id); err != nil {
			return err
		}
		fmt.Println("Device revoked; active connections close on the next heartbeat or call.")
	}
	return nil
}

func pairRoutes(next http.Handler, dir, port string, tailscale bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/device/pair" {
			servePair(w, r, dir, port, tailscale)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func servePair(w http.ResponseWriter, r *http.Request, dir, port string, tailscale bool) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	host, _, splitErr := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if r.Method != http.MethodPost || splitErr != nil || ip == nil || !ip.IsLoopback() || r.Header.Get("Origin") != "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var body struct {
		Agent   string `json:"agent"`
		Address string `json:"address"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Agent == "" {
		writePair(w, http.StatusBadRequest, "invalid", "Invalid pairing request.")
		return
	}
	name := lanHost()
	if tailscale {
		if tip, err := tailnet(); err == nil {
			name = identityHost(tip)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	s, err := device.Open(ctx, dir, name)
	if err != nil {
		writePair(w, http.StatusInternalServerError, "pair", "Couldn't open pairing.")
		return
	}
	defer s.Close()
	entered := []string{}
	if body.Address != "" {
		entered = append(entered, body.Address)
	}
	uri, err := s.IssueApp(ctx, pairCandidates(ctx, port, entered), body.Agent)
	var pe *device.PairError
	if errors.As(err, &pe) {
		writePair(w, http.StatusConflict, pe.Code, pairText(pe.Code))
		return
	}
	if err != nil {
		writePair(w, http.StatusInternalServerError, "pair", "Couldn't create a pairing code.")
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		URL string `json:"url"`
	}{URL: uri})
}

func pairText(code string) string {
	text := map[string]string{
		"no_address":   "Turn on Tailscale Serve or enter an address.",
		"pair_expired": "This code expired.",
		"pair_used":    "This code was already used.",
		"pair_locked":  "Too many attempts. Pairing is locked for 15 minutes.",
	}[code]
	if text == "" {
		return "Pairing failed."
	}
	return text
}

func writePair(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message})
}

func pairCandidates(ctx context.Context, port string, entered []string) []string {
	urls := []string{}
	if u := tailscalePairURL(ctx, port); u != "" {
		urls = append(urls, u)
	}
	return append(urls, entered...)
}

var (
	tsMu  sync.Mutex
	tsAt  time.Time
	tsURL string
)

func tailscalePairURL(ctx context.Context, port string) string {
	tsMu.Lock()
	defer tsMu.Unlock()
	if time.Since(tsAt) < 30*time.Second {
		return tsURL
	}
	tsAt = time.Now()
	tsURL = ""
	cctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	status, err := exec.CommandContext(cctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return ""
	}
	var body struct {
		Self struct{ DNSName string }
	}
	if json.Unmarshal(status, &body) != nil {
		return ""
	}
	for _, cmd := range []string{"serve", "funnel"} {
		out, err := exec.CommandContext(cctx, "tailscale", cmd, "status", "--json").Output()
		if err != nil {
			continue
		}
		if u := device.TailscaleURL(body.Self.DNSName, out, port); u != "" {
			tsURL = u
			return u
		}
	}
	return ""
}
