package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// useShellPath adds the user's shell PATH to the server's. launchd and systemd
// start it with a bare PATH, which would hide the user's tools from agents.
func useShellPath(ctx context.Context) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return nil
	}
	// Interactive, because many installers only edit the shell's rc file. The
	// path is the last line, after anything those startup files print.
	command := exec.CommandContext(ctx, shell, "-ilc", `printf '\n%s' "$PATH"`)
	// A new session leaves the shell no terminal to take from the server.
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("read PATH from %s: %w", shell, err)
	}
	line := output[bytes.LastIndexByte(output, '\n')+1:]
	return os.Setenv("PATH", joinPaths(string(line), os.Getenv("PATH")))
}

// joinPaths merges PATH lists in order, keeping the first of each directory.
func joinPaths(lists ...string) string {
	dirs := []string{}
	for _, list := range lists {
		for _, dir := range filepath.SplitList(list) {
			if filepath.IsAbs(dir) && !slices.Contains(dirs, dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}
