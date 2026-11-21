package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// cuaDriver drives the desktop and the browser through one long-lived `cua-driver mcp` session.
// One lane owns the connection, including initialization. No action is retried.
type cuaDriver struct {
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

func (d *cuaDriver) acquire(ctx context.Context) error {
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

func (d *cuaDriver) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
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

func (d *cuaDriver) close() {
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

func (d *cuaDriver) Name() string { return "cua-driver" }

func (d *cuaDriver) Spec() Spec { return cuaSpec }

func (d *cuaDriver) Close() { d.close() }

func (d *cuaDriver) Tools(ctx context.Context) ([]driverTool, error) {
	answer, err := d.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var listed struct {
		Tools []driverTool `json:"tools"`
	}
	if err := json.Unmarshal(answer, &listed); err != nil {
		return nil, errors.New("cua-driver sent a tool list that can't be read")
	}
	return listed.Tools, nil
}

func (d *cuaDriver) Call(ctx context.Context, tool string, args map[string]any) (toolResult, error) {
	if tool == "browser_prepare" && args["allow_launch"] == true {
		args["profile"] = map[string]string{"mode": "isolated_new"}
	}
	answer, err := d.request(ctx, "tools/call", map[string]any{"name": tool, "arguments": args})
	if err != nil {
		return toolResult{}, err
	}
	return render(answer), nil
}

// render turns an MCP tool result into text and pictures.
func render(answer json.RawMessage) toolResult {
	var result struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}
	if err := json.Unmarshal(answer, &result); err != nil {
		return toolResult{Text: "cua-driver sent an answer that can't be read.", IsError: true}
	}
	var texts []string
	var images []image
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			texts = append(texts, block.Text)
		case "image":
			images = append(images, image{Data: block.Data, MimeType: block.MimeType})
		}
	}
	if len(texts) == 0 && len(result.Structured) > 0 && string(result.Structured) != "null" {
		texts = append(texts, string(result.Structured))
	}
	text := strings.Join(texts, "\n")
	if result.IsError {
		text = "Error: " + text
	}
	if text == "" {
		text = "Done."
	}
	if string(result.Structured) == "null" {
		result.Structured = nil
	}
	return toolResult{Text: text, Images: images, Structured: result.Structured, IsError: result.IsError}
}

// cuaSpec is what the model may ask cua-driver for. Only single, inspectable calls are exposed: no
// scripts, nested steps, clipboard, file transfer, process termination or driver configuration.
var cuaSpec = Spec{
	Reads: map[string]string{
		"list_apps": "", "list_windows": "app pid",
		"get_screen_size": "", "get_desktop_state": "max_image_dimension",
		"get_window_state":  "pid window_id query since max_elements max_depth tree_format include_screenshot include_accessibility_tree max_image_dimension",
		"get_browser_state": "pid window_id tab_id target_id query include_screenshot snapshot_format scope_ref continuation",
	},
	Actions: map[string]string{
		"launch_app":       "bundle_id",
		"click":            "pid window_id element_token x y",
		"type_text":        "pid window_id element_token text",
		"press_key":        "pid window_id element_token key modifiers",
		"scroll":           "pid window_id x y direction amount",
		"browser_prepare":  "pid window_id strategy allow_launch",
		"browser_navigate": "tab_id target_id url",
		"browser_click":    "tab_id target_id ref x y",
		"browser_type":     "tab_id target_id ref text",
	},
	Check:   checkCua,
	Missing: "If a call says cua-driver isn't running or lacks permission, tell the user to open CuaDriver.app on that computer and grant Accessibility and Screen Recording.",
}

func checkCua(tool string, args map[string]any) string {
	positive := func(key string) bool {
		value, ok := args[key].(float64)
		return ok && value > 0 && value <= 1<<32-1 && value == float64(uint32(value))
	}
	switch tool {
	case "click", "type_text", "press_key", "scroll", "get_window_state":
		if !positive("pid") || !positive("window_id") {
			return "An exact pid and window_id are required."
		}
	case "browser_prepare":
		if args["allow_launch"] == true && len(args) == 1 {
			break // The driver chooses an attested browser and a new isolated profile.
		}
		strategy, _ := args["strategy"].(map[string]any)
		if len(args) != 3 || len(strategy) != 1 || strategy["kind"] != "existing_profile" ||
			!positive("pid") || !positive("window_id") {
			return "Prepare an isolated browser with allow_launch:true, or specify an exact window and the existing_profile strategy."
		}
	case "browser_navigate", "browser_click", "browser_type":
		target, _ := args["target_id"].(string)
		tab, _ := args["tab_id"].(string)
		if target == "" || tab == "" {
			return "An exact target_id and tab_id are required."
		}
	}
	if tool == "browser_navigate" {
		raw, _ := args["url"].(string)
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return "Browser navigation requires an HTTP(S) URL without credentials."
		}
	}
	return ""
}
