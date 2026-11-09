package acp

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

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
	// Text after a tool call starts a new paragraph instead of running on.
	if result.Text != "Reading the notes.\n\nHello from ACP [allow-once]" {
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

func TestAbortCancelsTheTurn(t *testing.T) {
	session := open(t, "")
	started := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	var result core.Result
	go func() {
		var err error
		result, err = session.Send(t.Context(), core.Message{Parts: []core.Part{{Kind: "text", Text: pointer(acptest.Wait)}}},
			func(core.Delta) error {
				once.Do(func() { close(started) })
				return nil
			})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the agent never streamed")
	}
	if err := session.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || result.Text != "Working" {
			t.Fatalf("send = %q, %v", result.Text, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("abort did not end the turn")
	}
	if !session.Reusable() {
		t.Fatal("a cancelled turn closed the session")
	}
}

func TestCrashFailsTheTurn(t *testing.T) {
	session := open(t, "")
	done := make(chan error, 1)
	go func() {
		_, err := session.Send(t.Context(), core.Message{Parts: []core.Part{{Kind: "text", Text: pointer(acptest.Crash)}}},
			func(core.Delta) error { return nil })
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a crashed agent reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a crashed agent left the turn hanging")
	}
	if session.Reusable() {
		t.Fatal("a crashed agent's session is reusable")
	}
}
