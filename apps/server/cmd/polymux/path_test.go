package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fakeShell(t *testing.T, body string) {
	t.Helper()
	shell := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
}

func TestShellPath(t *testing.T) {
	const bare = "/usr/bin:/service/bin"
	tests := []struct {
		name, shell, want string
		fails             bool
		limit             time.Duration
	}{
		{name: "adds the shell's tools", shell: `PATH=/login/bin:/usr/bin; eval "$2"`, want: "/login/bin:/usr/bin:/service/bin"},
		{name: "ignores startup output", shell: `echo Welcome; PATH=/login/bin; eval "$2"`, want: "/login/bin:/usr/bin:/service/bin"},
		{name: "keeps PATH on garbage", shell: `echo Welcome`, want: bare},
		{name: "keeps PATH on failure", shell: `exit 3`, want: bare, fails: true},
		{name: "keeps PATH on a hung profile", shell: `sleep 30`, want: bare, fails: true, limit: 200 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fakeShell(t, test.shell)
			t.Setenv("PATH", bare)
			limit := test.limit
			if limit == 0 {
				limit = 30 * time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), limit)
			defer cancel()
			if err := useShellPath(ctx); (err != nil) != test.fails {
				t.Fatal("unexpected result", err)
			}
			if got := os.Getenv("PATH"); got != test.want {
				t.Fatalf("PATH = %q, want %q", got, test.want)
			}
		})
	}
}
