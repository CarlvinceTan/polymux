package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// desktopDriver drives apps through agent-desktop, which reads each app's accessibility tree. It runs
// one command at a time, always headless: it never passes --headed or --force, so it can't take focus,
// move the pointer or send a shortcut the tool calls dangerous.
type desktopDriver struct {
	command string
	once    sync.Once
	session string
	lane    sync.Mutex
}

func desktopPath() string {
	if path := os.Getenv("POLYMUX_AGENT_DESKTOP"); path != "" {
		return path
	}
	return "agent-desktop"
}

func (d *desktopDriver) Name() string { return "agent-desktop" }

func (d *desktopDriver) Close() {}

var (
	refPattern    = regexp.MustCompile(`^@[a-z0-9]{1,32}:e[0-9]{1,6}$`)
	windowPattern = regexp.MustCompile(`^w-[0-9]{1,12}$`)
)

// desktopSpec is what the model may ask agent-desktop for. Argument names are the driver's own.
var desktopSpec = Spec{
	Reads: map[string]string{
		"list_apps":    "app",
		"list_windows": "app",
		"snapshot":     "app window_id root skeleton",
		"find":         "app window_id role name",
		"get":          "ref property",
		"is":           "ref property",
		"screenshot":   "app window_id",
	},
	Actions: map[string]string{
		"launch":    "app",
		"click":     "ref",
		"type":      "ref text",
		"press":     "combo app window_id",
		"scroll":    "ref direction amount",
		"set_value": "ref value",
		"select":    "ref value",
		"toggle":    "ref",
		"check":     "ref",
		"uncheck":   "ref",
	},
	Check:   checkDesktop,
	Missing: "If a call says agent-desktop isn't found or lacks permission, tell the user to run `agent-desktop permissions --guide` on that computer.",
}

func (d *desktopDriver) Spec() Spec { return desktopSpec }

// needsRef lists the tools that act on one element of a snapshot.
var needsRef = map[string]bool{
	"get": true, "is": true, "click": true, "type": true, "scroll": true, "set_value": true,
	"select": true, "toggle": true, "check": true, "uncheck": true,
}

func checkDesktop(tool string, args map[string]any) string {
	text := func(key string) string {
		value, _ := args[key].(string)
		return value
	}
	if value, _ := args["ref"].(string); needsRef[tool] && !refPattern.MatchString(value) {
		return "ref must be a qualified ref from snapshot, like @s8f3k2p9:e8."
	}
	if root, ok := args["root"]; ok {
		if value, _ := root.(string); !refPattern.MatchString(value) {
			return "root must be a qualified ref from snapshot."
		}
	}
	if id, ok := args["window_id"]; ok {
		if value, _ := id.(string); !windowPattern.MatchString(value) {
			return "window_id must come from list_windows, like w-2263."
		}
	}
	for _, key := range []string{"app", "role", "name", "combo", "property"} {
		if value, ok := args[key]; ok {
			s, isText := value.(string)
			if !isText || s == "" || len(s) > 200 || strings.HasPrefix(s, "-") {
				return key + " must be a short text that doesn't start with a dash."
			}
		}
	}
	for _, key := range []string{"text", "value"} {
		if value, ok := args[key]; ok {
			if s, isText := value.(string); !isText || len(s) > 10000 {
				return key + " must be text of at most 10,000 bytes."
			}
		}
	}
	switch tool {
	case "launch":
		if text("app") == "" {
			return "Name the app to launch."
		}
	case "press":
		if text("combo") == "" {
			return "Name the key combo to press, like cmd+c."
		}
	case "scroll":
		if direction, ok := args["direction"]; ok {
			if value, _ := direction.(string); value != "up" && value != "down" && value != "left" && value != "right" {
				return "direction must be up, down, left or right."
			}
		}
		if amount, ok := args["amount"]; ok {
			if value, _ := amount.(float64); value < 1 || value > 50 || value != float64(int(value)) {
				return "amount must be a whole number from 1 to 50."
			}
		}
	}
	return ""
}

func (d *desktopDriver) Tools(context.Context) ([]driverTool, error) {
	object := func(properties string, required ...string) json.RawMessage {
		req, _ := json.Marshal(required)
		return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{` + properties + `},"required":` + string(req) + `}`)
	}
	str := func(description string) string { return `{"type":"string","description":"` + description + `"}` }
	ref := `"ref":` + str("A qualified ref from snapshot, like @s8f3k2p9:e8.")
	app := `"app":` + str("An application name.")
	window := `"window_id":` + str("A window id from list_windows, like w-2263.")
	return []driverTool{
		{"list_apps", "List running apps. Pass app to filter.", object(app)},
		{"list_windows", "List windows with their ids, titles and bounds. Pass app to filter.", object(app)},
		{"snapshot", "Read an app's accessibility tree as JSON, with a ref for every element you can act on. Refs belong to their snapshot. Map a dense app with skeleton:true, then drill into a region with root.", object(app + `,` + window + `,"root":` + str("A ref to start from.") + `,"skeleton":{"type":"boolean"}`)},
		{"find", "Search elements by role or name.", object(app + `,` + window + `,"role":` + str("An accessibility role, like button.") + `,"name":` + str("An accessible name or label."))},
		{"get", "Read one property of a ref: text, value, title, bounds, role or states.", object(ref+`,"property":`+str("The property to read."), "ref")},
		{"is", "Check one state of a ref: visible, enabled, checked, focused, expanded or selected.", object(ref+`,"property":`+str("The state to check."), "ref")},
		{"screenshot", "Take a picture of an app's window.", object(app + `,` + window)},
		{"launch", "Open an app by name or bundle id, in the background.", object(app, "app")},
		{"click", "Press an element.", object(ref, "ref")},
		{"type", "Insert text into a text element.", object(ref+`,"text":`+str("The text to insert."), "ref", "text")},
		{"press", "Send a key combo such as return, escape or cmd+c to an app, without focusing it.", object(`"combo":`+str("The key combo.")+`,`+app+`,`+window, "combo")},
		{"scroll", "Scroll an element.", object(ref+`,"direction":{"type":"string","enum":["up","down","left","right"]},"amount":{"type":"integer","minimum":1,"maximum":50}`, "ref")},
		{"set_value", "Set an element's value directly.", object(ref+`,"value":`+str("The new value."), "ref", "value")},
		{"select", "Choose an option in a list or dropdown.", object(ref+`,"value":`+str("The option."), "ref", "value")},
		{"toggle", "Flip a checkbox or switch.", object(ref, "ref")},
		{"check", "Turn a checkbox or switch on.", object(ref, "ref")},
		{"uncheck", "Turn a checkbox or switch off.", object(ref, "ref")},
	}, nil
}

// desktopCommand maps a tool to the agent-desktop command and the arguments it takes.
func desktopCommand(tool string, args map[string]any) (name string, argv []string, stdin string) {
	text := func(key string) string {
		value, _ := args[key].(string)
		return value
	}
	flag := func(flag, key string) {
		if value := text(key); value != "" {
			argv = append(argv, flag, value)
		}
	}
	switch tool {
	case "list_apps":
		name = "list-apps"
		flag("--app", "app")
	case "list_windows":
		name = "list-windows"
		flag("--app", "app")
	case "snapshot":
		name = "snapshot"
		argv = append(argv, "-i", "--compact")
		flag("--app", "app")
		flag("--window-id", "window_id")
		flag("--root", "root")
		if args["skeleton"] == true {
			argv = append(argv, "--skeleton")
		}
	case "find":
		name = "find"
		flag("--app", "app")
		flag("--window-id", "window_id")
		flag("--role", "role")
		flag("--name", "name")
	case "get", "is":
		name = tool
		flag("--property", "property")
		argv = append(argv, "--", text("ref"))
	case "screenshot":
		name = "screenshot"
		flag("--app", "app")
		flag("--window-id", "window_id")
	case "launch":
		name = "launch"
		argv = append(argv, "--", text("app"))
	case "click", "toggle", "check", "uncheck":
		name = tool
		argv = append(argv, "--", text("ref"))
	case "type":
		name, stdin = "type", text("text")
		argv = append(argv, "--stdin", "--", text("ref"))
	case "set_value":
		name, stdin = "set-value", text("value")
		argv = append(argv, "--stdin", "--", text("ref"))
	case "select":
		name = "select"
		argv = append(argv, "--", text("ref"), text("value"))
	case "press":
		name = "press"
		flag("--app", "app")
		flag("--window-id", "window_id")
		argv = append(argv, "--", text("combo"))
	case "scroll":
		name = "scroll"
		flag("--direction", "direction")
		if amount, ok := args["amount"].(float64); ok {
			argv = append(argv, "--amount", strconv.Itoa(int(amount)))
		}
		argv = append(argv, "--", text("ref"))
	}
	return name, argv, stdin
}

// maxText keeps one snapshot from filling the model's context.
const maxText = 120_000

func (d *desktopDriver) Call(ctx context.Context, tool string, args map[string]any) (toolResult, error) {
	d.once.Do(func() { d.session = "polymux-" + rand.Text() })
	name, argv, stdin := desktopCommand(tool, args)
	if name == "" {
		return toolResult{}, errors.New("agent-desktop has no tool " + tool)
	}
	d.lane.Lock()
	defer d.lane.Unlock()
	cmd := exec.CommandContext(ctx, d.command, append([]string{name, "--session", d.session}, argv...)...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return toolResult{}, fmt.Errorf("driver call canceled; a dispatched action may already have run: %w", ctx.Err())
	}
	var envelope struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(out.Bytes(), &envelope) != nil {
		var missing *exec.Error
		if errors.As(runErr, &missing) || errors.Is(runErr, os.ErrNotExist) {
			return toolResult{}, errors.New("can't start agent-desktop: " + runErr.Error())
		}
		return toolResult{}, fmt.Errorf("agent-desktop gave no answer: %s", strings.TrimSpace(errs.String()))
	}
	if !envelope.OK {
		message := "failed"
		if envelope.Error != nil {
			message = envelope.Error.Code + ": " + envelope.Error.Message
		}
		return toolResult{Text: "Error: " + message, IsError: true}, nil
	}
	if tool == "screenshot" {
		var shot struct {
			Data string `json:"data"`
		}
		if json.Unmarshal(envelope.Data, &shot) == nil && shot.Data != "" {
			return toolResult{Text: "Screenshot taken.", Images: []image{{Data: shot.Data, MimeType: "image/png"}}}, nil
		}
	}
	text, structured := string(envelope.Data), envelope.Data
	if len(text) > maxText {
		text, structured = text[:maxText]+"\n[cut: read a smaller region with root, or a skeleton]", nil
	}
	if text == "" || text == "null" {
		text, structured = "Done.", nil
	}
	return toolResult{Text: text, Structured: structured}, nil
}
