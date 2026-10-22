package server

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMediaCleanup(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	dir := filepath.Join(api.config.DataDir, "media-jobs")
	owned := mediaJob{ID: strings.Repeat("1", 32), Owner: "assistant", Conversation: "assistant", Kind: "video", Status: "pending", Message: "reply", Call: "call"}
	other := owned
	other.ID, other.Owner, other.Conversation = strings.Repeat("2", 32), "other", "other"
	invalid := owned
	invalid.ID = "../outside"
	for _, job := range []mediaJob{owned, other} {
		if err := saveMediaJob(filepath.Join(dir, job.ID+".json"), job); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, job.ID+".bin"), []byte(job.Owner), 0600); err != nil {
			t.Fatal(err)
		}
	}
	invalidID := strings.Repeat("3", 32)
	if err := saveMediaJob(filepath.Join(dir, invalidID+".json"), invalid); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, invalidID+".bin"), []byte("unattributable"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, owned.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.Store.DeleteAgent(t.Context(), owned.Owner); err != nil {
		t.Fatal(err)
	}
	// A job already selected by the worker must stop at the deleted membership fence.
	api.completeMedia(t.Context(), dir, owned.ID, time.Now())
	after, err := os.ReadFile(filepath.Join(dir, owned.ID+".json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("worker rewrote a deleted owner's job", err)
	}
	for range 2 {
		if err := api.removeMedia(owned.Owner); err != nil {
			t.Fatal(err)
		}
	}
	// A stale scan cannot recreate files after cleanup has removed the journal.
	api.completeMedia(t.Context(), dir, owned.ID, time.Now())
	for _, suffix := range []string{".json", ".bin"} {
		if _, err := os.Stat(filepath.Join(dir, owned.ID+suffix)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("deleted media remains or was recreated", suffix, err)
		}
		for _, id := range []string{other.ID, invalidID} {
			if _, err := os.Stat(filepath.Join(dir, id+suffix)); err != nil {
				t.Fatal("another owner's or unattributable media was removed", id, suffix, err)
			}
		}
	}
}
