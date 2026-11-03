package server

import (
	"context"
	"testing"
	"time"
)

// waits reports whether take is still blocked after a moment.
func waits(t *testing.T, done <-chan error) bool {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		return false
	case <-time.After(50 * time.Millisecond):
		return true
	}
}

func taking(slots *turnSlots, ctx context.Context, urgent bool) <-chan error {
	done := make(chan error, 1)
	go func() { done <- slots.take(ctx, urgent) }()
	return done
}

// Background turns leave a slot for the user, and the user's turns go first when slots free up.
func TestTurnSlotsServeTheUserFirst(t *testing.T) {
	slots := newTurnSlots(2)
	if waits(t, taking(slots, t.Context(), false)) {
		t.Fatal("the first background turn should start")
	}
	second := taking(slots, t.Context(), false)
	if !waits(t, second) {
		t.Fatal("background turns filled the slot kept for the user")
	}
	if waits(t, taking(slots, t.Context(), true)) {
		t.Fatal("the user's turn waited for a slot")
	}
	queued := taking(slots, t.Context(), true)
	if !waits(t, queued) {
		t.Fatal("more turns ran than there are slots")
	}
	slots.give()
	if waits(t, queued) {
		t.Fatal("a freed slot didn't go to the waiting user turn")
	}
	if !waits(t, second) {
		t.Fatal("a background turn went ahead of the user")
	}
	slots.give()
	slots.give()
	if waits(t, second) {
		t.Fatal("the background turn never started")
	}
}

func TestTurnSlotsCancelledWaitLeaves(t *testing.T) {
	slots := newTurnSlots(1)
	if err := slots.take(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancelled := taking(slots, ctx, true)
	cancel()
	if err := <-cancelled; err == nil {
		t.Fatal("a cancelled wait took a slot")
	}
	slots.give()
	if waits(t, taking(slots, t.Context(), true)) {
		t.Fatal("the slot was lost to the cancelled wait")
	}
}
