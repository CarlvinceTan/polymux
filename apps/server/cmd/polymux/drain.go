package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type maintenance struct {
	Build    string `json:"buildSHA"`
	Instance string `json:"instance"`
	State    string `json:"state"`
	Active   int    `json:"active"`
	Blocked  int    `json:"blocked"`
}

func drain(args []string) error {
	flags := flag.NewFlagSet("drain", flag.ContinueOnError)
	base := flags.String("url", "", "server URL")
	tokenFile := flags.String("token-file", "", "owner token file")
	id := flags.String("id", "", "unique update identity, required to begin or cancel")
	cancel := flags.Bool("cancel", false, "resume admission for this update")
	status := flags.Bool("status", false, "show maintenance state")
	build := flags.String("build", "", "require this build identity when checking status")
	sealed := flags.Bool("ready", false, "require sealed admission when checking status")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !*status && (len(*id) < 16 || len(*id) > 128) {
		return errors.New("usage: polymux drain [--url URL] [--token-file PATH] (--status | --id ID [--cancel])")
	}
	if *base == "" {
		*base = defaultPairURL()
	}
	token, err := readToken(*tokenFile)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	call := func(action string) (maintenance, error) {
		return maintenanceCall(ctx, *base, token, *id, action)
	}
	if *status || *cancel {
		action := ""
		if *cancel {
			action = "cancel"
		}
		value, err := call(action)
		if err != nil {
			return err
		}
		if *build != "" && value.Build != *build {
			return errors.New("server build differs from the candidate")
		}
		if *sealed && (value.State != "ready" || value.Active != 0) {
			return errors.New("candidate admission is not sealed")
		}
		return json.NewEncoder(os.Stdout).Encode(value)
	}
	initial, err := call("begin")
	if err != nil {
		return err
	}
	ready := false
	defer func() {
		if ready {
			return
		}
		cleanup, end := context.WithTimeout(context.Background(), 5*time.Second)
		defer end()
		if _, err := maintenanceCall(cleanup, *base, token, *id, "cancel"); err != nil {
			fmt.Fprintln(os.Stderr, "Could not resume admission; inspect polymux drain --status before retrying.")
		}
	}()
	fmt.Fprintln(os.Stderr, "Update staged; waiting for active work. New messages will be queued.")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		value, err := call("seal")
		if err != nil {
			return err
		}
		if initial.Instance == "" || value.Instance != initial.Instance {
			return errors.New("server process changed while draining; refusing restart")
		}
		if value.State == "ready" {
			if value.Active != 0 || value.Blocked != 0 {
				return errors.New("ready server still reports active work")
			}
			ready = true
			return json.NewEncoder(os.Stdout).Encode(value)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func maintenanceCall(ctx context.Context, base, token, id, action string) (maintenance, error) {
	var value maintenance
	body, err := json.Marshal(map[string]string{"id": id, "action": action})
	if err != nil {
		return value, err
	}
	method := http.MethodPost
	if action == "" {
		method = http.MethodGet
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+"/maintenance", bytes.NewReader(body))
	if err != nil {
		return value, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(request)
	if err != nil {
		return value, fmt.Errorf("reach maintenance API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return value, fmt.Errorf("maintenance API returned HTTP %d; refusing an unsafe restart", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&value); err != nil {
		return value, err
	}
	switch value.State {
	case "running", "draining", "ready":
		return value, nil
	default:
		return value, errors.New("invalid maintenance response; refusing an unsafe restart")
	}
}
