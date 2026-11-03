package server

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
)

func TestSilence(t *testing.T) {
	const limit = 10 * time.Minute
	check := func(name string, want bool, steps func(*silence)) {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var stopped atomic.Bool
				quiet := newSilence(limit, func() { stopped.Store(true) })
				steps(quiet)
				synctest.Wait()
				if stopped.Load() != want {
					t.Fatalf("stopped = %v, want %v", stopped.Load(), want)
				}
				quiet.end()
			})
		})
	}
	text, start, done := agentruntime.Delta{Text: "more"}, agentruntime.Delta{Kind: "tool.start"}, agentruntime.Delta{Kind: "tool.done"}
	check("a silent model stops", true, func(*silence) { time.Sleep(limit + time.Second) })
	check("progress keeps it going", false, func(q *silence) {
		for range 12 {
			time.Sleep(limit - time.Second)
			q.saw(text)
		}
	})
	check("a long tool is not silence", false, func(q *silence) {
		q.saw(start)
		time.Sleep(6 * time.Hour)
	})
	check("every parallel tool must finish", false, func(q *silence) {
		q.saw(start)
		q.saw(start)
		q.saw(done)
		time.Sleep(6 * time.Hour)
	})
	check("silence after a tool stops", true, func(q *silence) {
		q.saw(start)
		time.Sleep(time.Hour)
		q.saw(done)
		time.Sleep(limit + time.Second)
	})
	check("a resumed tool result does not underflow", true, func(q *silence) {
		q.saw(done)
		time.Sleep(limit + time.Second)
	})
	check("waiting for a provider is not silence", false, func(q *silence) {
		q.saw(agentruntime.Delta{Kind: "waiting"})
		time.Sleep(48 * time.Hour)
	})
	check("silence after the wait stops", true, func(q *silence) {
		q.saw(agentruntime.Delta{Kind: "waiting"})
		time.Sleep(time.Hour)
		q.saw(text)
		time.Sleep(limit + time.Second)
	})
	check("a finished turn never stops", false, func(q *silence) {
		q.end()
		time.Sleep(limit + time.Second)
	})
}
