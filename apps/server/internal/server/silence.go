package server

import (
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
)

// silence stops a turn the model has gone quiet on. Progress restarts the clock
// and a running tool pauses it, because tools may take as long as they need.
type silence struct {
	mu    sync.Mutex
	timer *time.Timer
	limit time.Duration
	tools int
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
	if q.tools > 0 {
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
