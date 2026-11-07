package server

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type shutdownSession struct {
	*testSession
	closed atomic.Int32
}

func (s *shutdownSession) Close() error {
	s.closed.Add(1)
	return nil
}

func TestDeletionShutdownClosesSession(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	session := &shutdownSession{testSession: &testSession{factory: &testFactory{}}}
	api.actors["assistant"].mu.Lock()
	api.actors["assistant"].session = session
	api.actors["assistant"].mu.Unlock()
	cancelled, drained, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var cancelOnce, releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	api.actorsMu.Lock()
	api.voices["draining"] = &voiceWork{
		targets: map[string]bool{"assistant": true}, done: drained,
		cancel: func() { cancelOnce.Do(func() { close(cancelled) }) },
	}
	api.workers.Add(1)
	api.actorsMu.Unlock()
	go func() {
		<-release
		close(drained)
		api.workers.Done()
	}()
	deleted := make(chan error, 1)
	go func() { deleted <- api.eraseAgent(t.Context(), "assistant") }()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("deletion did not reach its private consultation cleanup")
	}
	closed := make(chan error, 1)
	go func() { closed <- api.Close() }()
	<-api.ctx.Done()
	var deleteErr error
	early := false
	select {
	case deleteErr = <-deleted:
		early = true
	case <-time.After(100 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after its private consultation drained")
	}
	if !early {
		deleteErr = <-deleted
	}
	if early || deleteErr != nil || session.closed.Load() != 1 {
		t.Fatalf("deletion lost session cleanup during shutdown: early=%v err=%v closes=%d", early, deleteErr, session.closed.Load())
	}
}
