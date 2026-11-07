package acp

import (
	"os"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/acptest"
	core "github.com/CarlvinceTan/polymux/packages/runtime"
)

func TestMain(m *testing.M) {
	if os.Getenv(acptest.Env) == "1" {
		acptest.Serve()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func open(t *testing.T, sessionID string) *Session {
	t.Helper()
	t.Setenv(acptest.Env, "1")
	session, err := Open(t.Context(), Config{
		Command:   os.Args[0],
		Args:      []string{"-test.run=^$"},
		Cwd:       t.TempDir(),
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestSendStreamsTextAndTools(t *testing.T) {
	session := open(t, "")
	if session.Path() != acptest.Created {
		t.Fatalf("path = %q", session.Path())
	}
	var deltas []core.Delta
	result, err := session.Send(t.Context(), core.Message{Parts: []core.Part{{Kind: "text", Text: pointer("do it")}}},
		func(delta core.Delta) error {
			deltas = append(deltas, delta)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hello from ACP [allow-once]" {
		t.Fatalf("reply = %q", result.Text)
	}
	var tool, done bool
	for _, delta := range deltas {
		switch delta.Kind {
		case "tool.start":
			tool = delta.Tool == "Read file" && delta.ToolID == "t1" && delta.Brief == "notes.md"
		case "tool.done":
			done = delta.Tool == "Read file" && !delta.Failed && delta.Output == "notes"
		}
	}
	if !tool || !done {
		t.Fatalf("tool deltas = %+v", deltas)
	}
}

func TestOpenLoadsKnownSession(t *testing.T) {
	if session := open(t, "known"); session.Path() != "known" {
		t.Fatalf("path = %q", session.Path())
	}
}

func TestOpenStartsFreshWhenSessionIsMissing(t *testing.T) {
	if session := open(t, "missing"); session.Path() != acptest.Created {
		t.Fatalf("path = %q", session.Path())
	}
}

func TestCloseLetsTheAgentExit(t *testing.T) {
	session := open(t, "")
	if err := session.Close(); err != nil {
		t.Fatalf("close = %v", err)
	}
	if session.Reusable() {
		t.Fatal("a closed session is reusable")
	}
}

func TestSendRejectsAnEmptyPrompt(t *testing.T) {
	session := open(t, "")
	if _, err := session.Send(t.Context(), core.Message{}, func(core.Delta) error { return nil }); err == nil {
		t.Fatal("expected an error")
	}
}

func pointer(text string) *string { return &text }
