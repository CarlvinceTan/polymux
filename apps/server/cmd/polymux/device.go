package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
)

func runDevice(args []string) error {
	if len(args) == 0 || args[0] != "pair" && args[0] != "list" && args[0] != "revoke" {
		return errors.New("usage: polymux-server device pair|list|revoke [-data path] [-url https://IP:7339] [-agent ID] [-id ID] [-listen IP:7337] [-remote URL]")
	}
	flags := flag.NewFlagSet("device", flag.ContinueOnError)
	dir := flags.String("data", os.Getenv("POLYMUX_DATA_DIR"), "server data directory")
	address := flags.String("url", "", "pinned TLS device listener")
	agent := flags.String("agent", "", "agent the linked app works with")
	id := flags.String("id", "", "device to revoke")
	listen := flags.String("listen", os.Getenv("POLYMUX_LISTEN"), "running server's API listener, asked for transport addresses (defaults to 127.0.0.1:7337)")
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
	if *listen == "" {
		*listen = "127.0.0.1:7337"
	}
	if *address == "" {
		*address = "https://" + net.JoinHostPort(lanHost(), "7339")
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
		entered := []string{}
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "url" {
				entered = append(entered, *address)
			}
		})
		if *remote != "" {
			entered = append(entered, *remote)
		}
		if v2, err := s.IssueApp(ctx, pairCandidates(ctx, *dir, *listen, entered), *agent); err == nil {
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

func pairCandidates(ctx context.Context, dir, api string, entered []string) []string {
	return append(transportDevices(ctx, dir, api), entered...)
}

// transportDevices asks the server where its transport plugins pass the device listener through.
func transportDevices(ctx context.Context, dir, api string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "devices", "local-token"))
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+api+"/transports", nil)
	if err != nil {
		return nil
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(data)))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	var reports []struct{ State, Device string }
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&reports) != nil {
		return nil
	}
	var urls []string
	for _, report := range reports {
		if report.State == "ready" && report.Device != "" {
			urls = append(urls, report.Device)
		}
	}
	return urls
}
