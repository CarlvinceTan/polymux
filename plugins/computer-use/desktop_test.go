package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeDesktop is a stand-in for agent-desktop that records its arguments and stdin.
func fakeDesktop(t *testing.T, answer string) (*desktopDriver, string) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := filepath.Join(dir, "agent-desktop")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\ncat >> " + log + ".stdin\nprintf '%s' '" + answer + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return &desktopDriver{command: script}, log
}

func TestDesktopRefusesWhatItShouldNotRun(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{`{"tool":"snapshot","args":{"app":"Finder","skeleton":true}}`, true},
		{`{"tool":"click","args":{"ref":"@s8f3k2p9:e8"}}`, true},
		{`{"tool":"click","args":{"ref":"e8"}}`, false},                         // refs belong to a snapshot
		{`{"tool":"click","args":{}}`, false},                                   // an action needs its target
		{`{"tool":"click","args":{"ref":"@s8f3k2p9:e8","headed":true}}`, false}, // never physical input
		{`{"tool":"press","args":{"combo":"cmd+c","force":true}}`, false},       // never forced shortcuts
		{`{"tool":"launch","args":{"app":"--help"}}`, false},                    // no flag injection
		{`{"tool":"list_windows","args":{"app":"-x"}}`, false},
		{`{"tool":"scroll","args":{"ref":"@s8f3k2p9:e8","direction":"sideways"}}`, false},
		{`{"tool":"scroll","args":{"ref":"@s8f3k2p9:e8","amount":500}}`, false},
		{`{"tool":"click","args":{"ref":"@s8f3k2p9:e8","window_id":"../x"}}`, false},
		{`{"tool":"run_steps","args":{}}`, false},
		{`{"tool":"clipboard_set","args":{"text":"x"}}`, false},
	} {
		var args input
		if err := json.Unmarshal([]byte(tc.raw), &args); err != nil {
			t.Fatal(err)
		}
		if reason := desktopSpec.check(args); (reason == "") != tc.ok {
			t.Errorf("%s: %q", tc.raw, reason)
		}
	}
}

func TestDesktopActionsNeedApprovalAndReadsDont(t *testing.T) {
	for _, tool := range []string{"snapshot", "find", "get", "is", "screenshot", "list_apps", "list_windows"} {
		if desktopSpec.isAction(tool) {
			t.Errorf("%s only reads", tool)
		}
	}
	for _, tool := range []string{"click", "type", "press", "scroll", "set_value", "select", "toggle", "check", "uncheck", "launch"} {
		if !desktopSpec.isAction(tool) {
			t.Errorf("%s acts and needs approval", tool)
		}
	}
}

func TestDesktopSendsTextOnStdinAndNeverHeadedOrForced(t *testing.T) {
	d, log := fakeDesktop(t, `{"ok":true,"data":{"done":true}}`)
	call := func(tool, args string) {
		var parsed map[string]any
		_ = json.Unmarshal([]byte(args), &parsed)
		if _, err := d.Call(t.Context(), tool, parsed); err != nil {
			t.Fatal(err)
		}
	}
	call("type", `{"ref":"@s1:e2","text":"--headed secret"}`)
	call("press", `{"combo":"cmd+c","app":"Notes"}`)
	call("snapshot", `{"app":"Finder","skeleton":true}`)
	data, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("commands: %q", lines)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "--headed") || strings.Contains(string(data), "--force") {
		t.Fatalf("text or physical flags reached the command line:\n%s", data)
	}
	stdin, _ := os.ReadFile(log + ".stdin")
	if string(stdin) != "--headed secret" {
		t.Fatalf("stdin %q", stdin)
	}
	if !strings.HasPrefix(lines[0], "type --session polymux-") || !strings.Contains(lines[0], "--stdin -- @s1:e2") {
		t.Fatalf("type: %q", lines[0])
	}
	if !strings.Contains(lines[2], "--skeleton") || !strings.Contains(lines[2], "--app Finder") {
		t.Fatalf("snapshot: %q", lines[2])
	}
	// One session for the driver's life, so refs from one call work in the next.
	session := strings.Fields(lines[0])[2]
	for _, line := range lines {
		if !slices.Contains(strings.Fields(line), session) {
			t.Fatalf("session changed: %q", line)
		}
	}
}

func TestDesktopReturnsPicturesAndErrors(t *testing.T) {
	d, _ := fakeDesktop(t, `{"ok":true,"data":{"data":"aGk="}}`)
	shot, err := d.Call(t.Context(), "screenshot", map[string]any{"app": "Finder"})
	if err != nil || len(shot.Images) != 1 || shot.Images[0].MimeType != "image/png" || shot.Images[0].Data != "aGk=" {
		t.Fatalf("screenshot %+v, %v", shot, err)
	}
	d, _ = fakeDesktop(t, `{"ok":false,"error":{"code":"STALE_REF","message":"take a new snapshot"}}`)
	failed, err := d.Call(t.Context(), "click", map[string]any{"ref": "@s1:e2"})
	if err != nil || !failed.IsError || !strings.Contains(failed.Text, "STALE_REF") {
		t.Fatalf("failed %+v, %v", failed, err)
	}
	missing := &desktopDriver{command: "/nonexistent/agent-desktop"}
	if _, err := missing.Call(t.Context(), "list_apps", map[string]any{}); err == nil || !strings.Contains(err.Error(), "can't start") {
		t.Fatalf("missing binary: %v", err)
	}
}

func TestDriverIsChosenByName(t *testing.T) {
	t.Setenv("POLYMUX_COMPUTER_DRIVER", "agent-desktop")
	if d, err := newDriver(); err != nil || d.Name() != "agent-desktop" {
		t.Fatalf("%v, %v", d, err)
	}
	t.Setenv("POLYMUX_COMPUTER_DRIVER", "")
	if d, err := newDriver(); err != nil || d.Name() != "cua-driver" {
		t.Fatalf("default %v, %v", d, err)
	}
	t.Setenv("POLYMUX_COMPUTER_DRIVER", "nope")
	if _, err := newDriver(); err == nil {
		t.Fatal("an unknown driver was accepted")
	}
}
