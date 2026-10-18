package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

type Config struct {
	Binary  string
	Args    []string
	Dir     string
	Output  *os.File
	Restart time.Duration
}

type Controller struct {
	mu     sync.Mutex
	state  contract.HostState
	config Config
	wake   chan struct{}
}

func Open(config Config) (*Controller, error) {
	if !filepath.IsAbs(config.Binary) || !filepath.IsAbs(config.Dir) {
		return nil, errors.New("host paths must be absolute")
	}
	if config.Restart <= 0 {
		config.Restart = 5 * time.Second
	}
	if config.Output == nil {
		config.Output = os.Stderr
	}
	c := &Controller{config: config, state: contract.HostState{Enabled: true}, wake: make(chan struct{}, 1)}
	data, err := os.ReadFile(filepath.Join(config.Dir, "host.json"))
	if errors.Is(err, os.ErrNotExist) {
		if err := c.save(true); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		var persisted struct {
			Enabled *bool `json:"enabled"`
		}
		if json.Unmarshal(data, &persisted) != nil || persisted.Enabled == nil {
			return nil, errors.New("invalid host configuration")
		}
		c.state.Enabled = *persisted.Enabled
	}
	return c, nil
}

func (c *Controller) Snapshot() contract.HostState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Controller) Enable(enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.save(enabled); err != nil {
		return err
	}
	c.state.Enabled = enabled
	c.state.Error = ""
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}

func (c *Controller) save(enabled bool) error {
	if err := os.MkdirAll(c.config.Dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(c.config.Dir, ".host-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(contract.HostPatch{Enabled: enabled})
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(c.config.Dir, "host.json"))
}

func (c *Controller) Run(ctx context.Context) {
	var process *exec.Cmd
	var exited chan error
	var next time.Time
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	stop := func() {
		if process == nil {
			return
		}
		_ = process.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(20 * time.Second):
			_ = process.Process.Kill()
			<-exited
		}
		process, exited = nil, nil
		c.mu.Lock()
		c.state.Running = false
		c.state.Pid = 0
		c.mu.Unlock()
	}
	defer stop()
	for {
		state := c.Snapshot()
		if !state.Enabled {
			stop()
			next = time.Time{}
		}
		if state.Enabled && process == nil && !time.Now().Before(next) && ctx.Err() == nil {
			process = exec.Command(c.config.Binary, c.config.Args...)
			process.Stdout, process.Stderr = c.config.Output, c.config.Output
			err := process.Start()
			c.mu.Lock()
			if err != nil {
				c.state.Error = "Couldn’t start the server. Check the host log."
				process = nil
				next = time.Now().Add(c.config.Restart)
			} else {
				c.state.Running = true
				c.state.Pid = int64(process.Process.Pid)
				c.state.Error = ""
				exited = make(chan error, 1)
				go func(cmd *exec.Cmd, done chan error) { done <- cmd.Wait() }(process, exited)
			}
			c.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-tick.C:
		case <-exited:
			process, exited = nil, nil
			c.mu.Lock()
			c.state.Running = false
			c.state.Pid = 0
			c.state.Error = "Server exited; restarting shortly."
			c.mu.Unlock()
			next = time.Now().Add(c.config.Restart)
		}
	}
}

func (c *Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path != "/host" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPatch {
		if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			w.WriteHeader(403)
			return
		}
		var patch struct {
			Enabled *bool `json:"enabled"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&patch) != nil || patch.Enabled == nil || decoder.Decode(new(any)) != io.EOF {
			w.WriteHeader(400)
			return
		}
		if err := c.Enable(*patch.Enabled); err != nil {
			w.WriteHeader(500)
			_ = json.NewEncoder(w).Encode(contract.Error{Code: "host", Message: "Couldn’t save server state."})
			return
		}
	} else if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, PATCH")
		w.WriteHeader(405)
		return
	}
	_ = json.NewEncoder(w).Encode(c.Snapshot())
}

const Label = "com.flarehq.polymux.app.host"

func Unit(binary, dir, listen, control string) ([]byte, error) {
	for _, path := range []string{binary, dir} {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("service paths must be absolute")
		}
	}
	escape := func(s string) string {
		var b []byte
		for _, r := range s {
			switch r {
			case '&':
				b = append(b, "&amp;"...)
			case '<':
				b = append(b, "&lt;"...)
			case '>':
				b = append(b, "&gt;"...)
			case '"':
				b = append(b, "&quot;"...)
			default:
				b = append(b, []byte(string(r))...)
			}
		}
		return string(b)
	}
	args := ""
	for _, value := range []string{binary, "host", "run", "-data", dir, "-listen", listen, "-control", control} {
		args += "<string>" + escape(value) + "</string>"
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + Label + `</string><key>ProgramArguments</key><array>` + args + `</array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>10</integer><key>StandardOutPath</key><string>` + escape(filepath.Join(dir, "host.log")) + `</string><key>StandardErrorPath</key><string>` + escape(filepath.Join(dir, "host.log")) + `</string></dict></plist>`), nil
}
