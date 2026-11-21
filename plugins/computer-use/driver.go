package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

const protocolVersion = "2025-06-18"

// driver is one long-lived `cua-driver mcp` session. Element tokens and sessions live as long as
// it does, so every call shares it; it restarts on the next call after it exits.
type driver struct {
	command string

	mu           sync.Mutex
	stdin        io.WriteCloser
	process      *exec.Cmd
	next         int64
	pending      map[int64]chan reply
	instructions string
}

type reply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type message struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	reply
}

// driverPath finds cua-driver: an override, the PATH, then the usual install places.
func driverPath() string {
	if path := os.Getenv("POLYMUX_CUA_DRIVER"); path != "" {
		return path
	}
	if path, err := exec.LookPath("cua-driver"); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	for _, path := range []string{
		"/Applications/CuaDriver.app/Contents/MacOS/cua-driver",
		filepath.Join(home, ".local/bin/cua-driver"),
		"/opt/homebrew/bin/cua-driver",
		"/usr/local/bin/cua-driver",
	} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "cua-driver"
}

// start launches the MCP process and completes the handshake. Called with mu held.
func (d *driver) start(ctx context.Context) error {
	cmd := exec.Command(d.command, "mcp")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cua-driver isn't installed or can't start (%v). Install it from https://github.com/trycua/cua and grant CuaDriver Accessibility and Screen Recording", err)
	}
	d.stdin, d.process, d.pending = stdin, cmd, map[int64]chan reply{}
	go d.read(cmd, stdout)
	result, err := d.send(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "polymux-computer-use", "version": "1"},
	})
	if err != nil {
		d.stop()
		return err
	}
	var hello struct {
		Instructions string `json:"instructions"`
	}
	_ = json.Unmarshal(result, &hello)
	d.instructions = hello.Instructions
	return d.write(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

// read routes each answer to its caller until the process exits, then fails what's still waiting.
func (d *driver) read(cmd *exec.Cmd, stdout io.Reader) {
	lines := bufio.NewScanner(stdout)
	lines.Buffer(make([]byte, 1<<20), 64<<20)
	for lines.Scan() {
		var m message
		if json.Unmarshal(lines.Bytes(), &m) != nil || m.ID == nil {
			continue
		}
		if m.Method != "" {
			// The driver asking us something: we offer nothing back.
			_ = d.writeLocked(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "not supported"}})
			continue
		}
		var id int64
		if json.Unmarshal(*m.ID, &id) != nil {
			continue
		}
		d.mu.Lock()
		waiting := d.pending[id]
		delete(d.pending, id)
		d.mu.Unlock()
		if waiting != nil {
			waiting <- m.reply
		}
	}
	_ = cmd.Wait()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.process == cmd {
		for id, waiting := range d.pending {
			waiting <- reply{Error: &struct {
				Message string `json:"message"`
			}{Message: "cua-driver stopped"}}
			delete(d.pending, id)
		}
		d.process, d.stdin = nil, nil
	}
}

// send writes one request and waits for its answer. Called with mu held; it's released while waiting.
func (d *driver) send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	d.next++
	id := d.next
	waiting := make(chan reply, 1)
	d.pending[id] = waiting
	if err := d.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		delete(d.pending, id)
		return nil, err
	}
	d.mu.Unlock()
	defer d.mu.Lock()
	select {
	case answer := <-waiting:
		if answer.Error != nil {
			return nil, errors.New(answer.Error.Message)
		}
		return answer.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *driver) write(value any) error {
	if d.stdin == nil {
		return errors.New("cua-driver stopped")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = d.stdin.Write(append(data, '\n'))
	return err
}

func (d *driver) writeLocked(value any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.write(value)
}

func (d *driver) stop() {
	if d.stdin != nil {
		_ = d.stdin.Close()
	}
	if d.process != nil && d.process.Process != nil {
		_ = d.process.Process.Kill()
	}
	d.process, d.stdin = nil, nil
}

// request runs one MCP method, starting the driver first if it isn't running.
func (d *driver) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.process == nil {
		if err := d.start(ctx); err != nil {
			return nil, err
		}
	}
	return d.send(ctx, method, params)
}

// guide is the driver's own instructions from the handshake.
func (d *driver) guide(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.process == nil {
		if err := d.start(ctx); err != nil {
			return "", err
		}
	}
	return d.instructions, nil
}

func (d *driver) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stop()
}
