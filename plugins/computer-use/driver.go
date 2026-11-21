package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Driver is the part of Computer Use that touches a desktop. The tool surface the model sees, the
// allowlist and the approval gate depend only on this interface, so a driver is replaced by adding
// an implementation here and naming it in POLYMUX_COMPUTER_DRIVER.
type Driver interface {
	Name() string
	// Spec says what the model may ask for. It is static, so the gate needs no running driver.
	Spec() Spec
	// Tools lists the driver's tools with their descriptions and schemas.
	Tools(ctx context.Context) ([]driverTool, error)
	// Call runs one tool that Spec allows.
	Call(ctx context.Context, tool string, args map[string]any) (toolResult, error)
	Close()
}

// Spec is a driver's allowlist: which tools only read, which act, and the arguments each accepts.
type Spec struct {
	// Reads and Actions map a tool to the argument names it accepts, separated by spaces.
	Reads, Actions map[string]string
	// Check adds driver-specific rules to the generic ones; it returns why a call is refused, or "".
	Check func(tool string, args map[string]any) string
	// Missing is what to tell the user when the driver is absent or lacks a permission.
	Missing string
}

type driverTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func (s Spec) offers(tool string) bool {
	_, read := s.Reads[tool]
	_, action := s.Actions[tool]
	return read || action
}

// isAction is whether a tool changes something, so it needs approval.
func (s Spec) isAction(tool string) bool {
	_, action := s.Actions[tool]
	return action
}

// accepts lists a tool's argument names.
func (s Spec) accepts(tool string) string {
	if keys, ok := s.Reads[tool]; ok {
		return keys
	}
	return s.Actions[tool]
}

// check is why a call is refused, or "" when it may run.
func (s Spec) check(i input) string {
	if !s.offers(i.Tool) {
		return "Computer Use doesn't offer that driver tool."
	}
	if s.isAction(i.Tool) {
		detail, _ := json.Marshal(i.Args)
		if len(detail) > 3500 {
			return "Shorten the action so its full arguments fit on the approval card."
		}
	}
	for key := range i.Args {
		if !slices.Contains(strings.Fields(s.accepts(i.Tool)), key) {
			return "Unsupported Computer Use argument: " + key
		}
	}
	if s.Check != nil {
		return s.Check(i.Tool, i.Args)
	}
	return ""
}

// newDriver picks the driver named in POLYMUX_COMPUTER_DRIVER; cua-driver is the default.
func newDriver() (Driver, error) {
	switch name := os.Getenv("POLYMUX_COMPUTER_DRIVER"); name {
	case "", "cua-driver":
		return &cuaDriver{command: driverPath()}, nil
	case "agent-desktop":
		return &desktopDriver{command: desktopPath()}, nil
	default:
		return nil, fmt.Errorf("unknown driver %q; use cua-driver or agent-desktop", name)
	}
}
