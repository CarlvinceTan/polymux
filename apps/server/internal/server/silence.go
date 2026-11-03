package server

import (
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
)

// silence stops a turn the model has gone quiet on. Progress restarts the clock, and a running tool or a wait for a
// provider to come back pauses it, because those take as long as they need.
type silence struct {
	mu      sync.Mutex
	timer   *time.Timer
	limit   time.Duration
	tools   int
	waiting bool
}

func newSilence(limit time.Duration, stop func()) *silence {
	return &silence{timer: time.AfterFunc(limit, stop), limit: limit}
}

func (q *silence) saw(delta agentruntime.Delta) {
	q.mu.Lock()
	defer q.mu.Unlock()
	switch delta.Kind {
	case "tool.start":
		q.tools++
	case "tool.done":
		q.tools = max(q.tools-1, 0)
	}
	q.waiting = delta.Kind == "waiting"
	if q.tools > 0 || q.waiting {
		q.timer.Stop()
	} else {
		q.timer.Reset(q.limit)
	}
}

func (q *silence) end() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.timer.Stop()
}
