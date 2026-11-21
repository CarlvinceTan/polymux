package store

import (
	"path/filepath"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func queueMessage(t *testing.T, s *Store, thread, id, text string) {
	t.Helper()
	request := contract.SendRequest{ID: id, Text: text, AttachmentIDs: []string{}}
	if _, _, err := s.Send(t.Context(), thread, request, []contract.Part{{Kind: "text", Text: &request.Text}}, thread); err != nil {
		t.Fatal(err)
	}
}

func TestQueuedInputsKeepAdmissionOrder(t *testing.T) {
	t.Parallel()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "A"}}); err != nil {
		t.Fatal(err)
	}
	run := beginStage(t, s, "root")
	queueMessage(t, s, "a", "steer-one", "first steer")
	queueMessage(t, s, "a", "steer-two", "second steer")
	if err := sealStage(t, s, &run, nil); err != nil {
		t.Fatal(err)
	}
	first, err := s.Job(t.Context(), "a")
	if err != nil || first != "steer-one" {
		t.Fatalf("first job = %q, %v", first, err)
	}
	if err := s.Finish(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	second, err := s.Job(t.Context(), "a")
	if err != nil || second != "steer-two" {
		t.Fatalf("second job = %q, %v", second, err)
	}
}
