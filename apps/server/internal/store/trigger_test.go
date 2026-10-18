package store

import (
	"errors"
	"sync"
	"testing"
)

func TestTriggerDeduplicatesConcurrentEvents(t *testing.T) {
	t.Parallel()
	s := runStore(t)
	trigger := Trigger{ID: "inbox", AgentID: "a", Source: "email", Prompt: "Summarize relevant new mail without sending anything", Enabled: true}
	if err := s.SaveTrigger(t.Context(), trigger); err != nil {
		t.Fatal(err)
	}
	event := TriggerEvent{ID: "provider-message-1", Source: "email", Text: "Untrusted incoming content"}
	var wg sync.WaitGroup
	jobs := make(chan string, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { job, err := s.Ingest(t.Context(), trigger.ID, event); jobs <- job; errs <- err })
	}
	wg.Wait()
	close(jobs)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	job := ""
	for id := range jobs {
		if job != "" && job != id {
			t.Fatal("different duplicate job IDs")
		}
		job = id
	}
	var count int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs WHERE id=?`, job).Scan(&count); err != nil || count != 1 {
		t.Fatalf("queued %d %v", count, err)
	}
	work, err := s.Work(t.Context(), job)
	if err != nil || work.Trigger != "event" {
		t.Fatalf("work %+v %v", work, err)
	}
	event.Text = "changed"
	if _, err := s.Ingest(t.Context(), trigger.ID, event); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed stable ID accepted %v", err)
	}
	event.ID = "new"
	event.Source = "calendar"
	if _, err := s.Ingest(t.Context(), trigger.ID, event); !errors.Is(err, ErrTrigger) {
		t.Fatalf("source crossed scope %v", err)
	}
	trigger.Enabled = false
	if err := s.SaveTrigger(t.Context(), trigger); err != nil {
		t.Fatal(err)
	}
	event.Source = "email"
	if _, err := s.Ingest(t.Context(), trigger.ID, event); !errors.Is(err, ErrTrigger) {
		t.Fatalf("disabled watcher queued %v", err)
	}
	trigger.AgentID = "b"
	if err := s.SaveTrigger(t.Context(), trigger); !errors.Is(err, ErrMembers) {
		t.Fatal("binding ownership changed")
	}
}
