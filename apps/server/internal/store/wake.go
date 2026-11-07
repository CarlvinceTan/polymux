package store

import "sync"

// hub wakes goroutines waiting on a key. Writers wake inside their transaction; on the store's
// single connection the woken read waits for the commit, so it sees the change.
type hub struct {
	mu    sync.Mutex
	waits map[string]chan struct{}
}

func (h *hub) wait(key string) <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.waits == nil {
		h.waits = map[string]chan struct{}{}
	}
	ch, ok := h.waits[key]
	if !ok {
		ch = make(chan struct{})
		h.waits[key] = ch
	}
	return ch
}

func (h *hub) wake(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.waits[key]; ok {
		close(ch)
		delete(h.waits, key)
	}
}

// ThreadChanged closes when the thread next gets an event or is deleted. Take it before reading.
func (s *Store) ThreadChanged(thread string) <-chan struct{} { return s.events.wait(thread) }

// SidebarChanged closes when the next sidebar event is written. Take it before reading.
func (s *Store) SidebarChanged() <-chan struct{} { return s.events.wait("") }

// JobQueued closes when the thread next gets a queued job. Take it before claiming.
func (s *Store) JobQueued(thread string) <-chan struct{} { return s.jobs.wait(thread) }
