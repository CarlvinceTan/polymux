package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// One lane owns the connection, including initialization. No action is retried.
type driver struct {
	command string
	once    sync.Once
	lane    chan struct{}
	child   *connection
}

type connection struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	out     io.ReadCloser
	lines   *bufio.Scanner
	next    int64
	session string
}

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

func (d *driver) acquire(ctx context.Context) error {
	d.once.Do(func() { d.lane = make(chan struct{}, 1) })
	select {
	case d.lane <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-d.lane
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func launch(command string) (*connection, error) {
	cmd := exec.Command(command, "mcp")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("can't start cua-driver: %w", err)
	}
	lines := bufio.NewScanner(out)
	lines.Buffer(make([]byte, 64<<10), 16<<20)
	return &connection{cmd: cmd, in: in, out: out, lines: lines, session: "polymux-" + rand.Text()}, nil
}

func (c *connection) send(method string, params any) (json.RawMessage, error) {
	c.next++
	id := c.next
	writer := json.NewEncoder(c.in)
	if err := writer.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for c.lines.Scan() {
		var response struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(c.lines.Bytes(), &response); err != nil {
			return nil, err
		}
		if response.Method != "" {
			if len(response.ID) != 0 {
				err := writer.Encode(map[string]any{"jsonrpc": "2.0", "id": response.ID, "error": map[string]any{
					"code": -32601, "message": "Not supported",
				}})
				if err != nil {
					return nil, err
				}
			}
			continue
		}
		if string(response.ID) != fmt.Sprint(id) {
			continue
		}
		if response.Error != nil {
			return nil, errors.New(response.Error.Message)
		}
		return response.Result, nil
	}
	if err := c.lines.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("cua-driver stopped")
}

func (c *connection) initialize() error {
	_, err := c.send("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "polymux-computer-use", "version": "1"},
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(c.in).Encode(map[string]string{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

func (c *connection) interrupt() {
	_ = c.cmd.Process.Kill()
	_ = c.in.Close()
	_ = c.out.Close()
}

func (d *driver) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := d.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-d.lane }()
	fresh := d.child == nil
	if fresh {
		child, err := launch(d.command)
		if err != nil {
			return nil, err
		}
		d.child = child
	}
	c := d.child
	stop := context.AfterFunc(ctx, c.interrupt)
	var err error
	if fresh {
		err = c.initialize()
	}
	var result json.RawMessage
	if err == nil && ctx.Err() == nil {
		if call, ok := params.(map[string]any); method == "tools/call" && ok {
			if args, ok := call["arguments"].(map[string]any); ok {
				args["session"] = c.session
			}
		}
		result, err = c.send(method, params)
	}
	if !stop() || err != nil || ctx.Err() != nil {
		c.interrupt()
		_ = c.cmd.Wait()
		d.child = nil
		if ctx.Err() != nil {
			err = fmt.Errorf("driver call canceled; a dispatched action may already have run: %w", ctx.Err())
		}
		return nil, err
	}
	return result, nil
}

func (d *driver) close() {
	_ = d.acquire(context.Background())
	defer func() { <-d.lane }()
	if c := d.child; c != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		stop := context.AfterFunc(ctx, c.interrupt)
		_, _ = c.send("tools/call", map[string]any{"name": "end_session", "arguments": map[string]any{"session": c.session}})
		stop()
		cancel()
		c.interrupt()
		_ = c.cmd.Wait()
		d.child = nil
	}
}
