package server

import (
	"context"
	"slices"
	"sync"
)

type execution struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// acquire takes the turn of one session, keyed by id, and one of the server's turn slots. A turn the user is waiting
// on is urgent: it gets the next free slot, and one slot is kept for such turns, so background work never fills the
// server.
func (s *Server) acquire(ctx context.Context, id string, urgent bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resource, _ := s.resources.LoadOrStore(id, make(chan struct{}, 1))
	gate := resource.(chan struct{})
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := s.slots.take(ctx, urgent); err != nil {
		<-gate
		return nil, err
	}
	return func() { s.slots.give(); <-gate }, nil
}

// turnSlots limits how many turns run at once, serving urgent turns first and keeping one slot for them.
type turnSlots struct {
	mu         sync.Mutex
	size, used int
	urgent     []chan struct{}
	background []chan struct{}
}

func newTurnSlots(size int) *turnSlots { return &turnSlots{size: size} }

// fits reports whether a turn may take a slot now: background turns leave one free while there's more than one.
func (t *turnSlots) fits(urgent bool) bool {
	if urgent {
		return t.used < t.size
	}
	return t.used < max(1, t.size-1)
}

func (t *turnSlots) take(ctx context.Context, urgent bool) error {
	t.mu.Lock()
	if t.fits(urgent) && len(t.urgent) == 0 && (urgent || len(t.background) == 0) {
		t.used++
		t.mu.Unlock()
		return nil
	}
	queue := &t.background
	if urgent {
		queue = &t.urgent
	}
	granted := make(chan struct{})
	*queue = append(*queue, granted)
	t.mu.Unlock()
	select {
	case <-granted:
		return nil
	case <-ctx.Done():
		t.mu.Lock()
		if i := slices.Index(*queue, granted); i >= 0 {
			*queue = slices.Delete(*queue, i, i+1)
			t.mu.Unlock()
			return ctx.Err()
		}
		// The slot was granted as the wait ended, so hand it on.
		t.mu.Unlock()
		t.give()
		return ctx.Err()
	}
}

func (t *turnSlots) give() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.used--
	for len(t.urgent) > 0 && t.fits(true) {
		close(t.urgent[0])
		t.urgent, t.used = t.urgent[1:], t.used+1
	}
	for len(t.urgent) == 0 && len(t.background) > 0 && t.fits(false) {
		close(t.background[0])
		t.background, t.used = t.background[1:], t.used+1
	}
}
