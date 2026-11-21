package store

import (
	"strings"
	"testing"
)

func TestWakeOwnerQueuesMainChat(t *testing.T) {
	s := openTest(t)
	if err := s.WakeOwner(t.Context(), "a", "task-1", "Watch sessions", "All merged."); err != nil {
		t.Fatal(err)
	}
	job, err := s.Job(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.Work(t.Context(), job)
	if err != nil || work.Trigger != "event" || work.Message.ThreadID != "a" || !strings.Contains(Text(work.Message), "Watch sessions") {
		t.Fatalf("wake %+v %v", work, err)
	}
}
