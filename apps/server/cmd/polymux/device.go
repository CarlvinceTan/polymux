package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
)

func runDevice(args []string) error {
	if len(args) == 0 || args[0] != "pair" && args[0] != "list" && args[0] != "revoke" {
		return errors.New("usage: polymux-server device pair|list|revoke [-data path] [-url https://IP:7339] [-agent ID] [-id ID] [-remote URL]")
	}
	flags := flag.NewFlagSet("device", flag.ContinueOnError)
	dir := flags.String("data", os.Getenv("POLYMUX_DATA_DIR"), "server data directory")
	address := flags.String("url", "", "pinned TLS device listener")
	agent := flags.String("agent", "", "agent the linked app works with")
	id := flags.String("id", "", "device to revoke")
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
		if v2, err := s.IssueApp(ctx, entered, *agent); err == nil {
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
