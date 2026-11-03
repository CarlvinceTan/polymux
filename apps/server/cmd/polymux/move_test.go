package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/link"
)

func stagedServer(t *testing.T, dir, marker string) string {
	t.Helper()
	pairs, err := device.Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer pairs.Close()
	if _, err := pairs.Local(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := link.LoadIdentity(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	return marker
}

func TestUncommittedCopyIsThrownAway(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	stagedServer(t, dir, "mine")
	if err := os.MkdirAll(dir+".incoming", 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := lockData(dir, ".server.lock")
	if err != nil {
		t.Fatal(err)
	}
	lock, err = activateIncoming(t.Context(), dir, lock)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if data, _ := os.ReadFile(filepath.Join(dir, "marker")); string(data) != "mine" {
		t.Fatal("an uncommitted move replaced the server's data")
	}
	if _, err := os.Stat(dir + ".incoming"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the leftover copy is still there")
	}
}

func TestCommittedCopyTakesOver(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	stagedServer(t, dir, "mine")
	incoming := dir + ".incoming"
	if err := os.MkdirAll(incoming, 0o700); err != nil {
		t.Fatal(err)
	}
	stagedServer(t, incoming, "moved")
	if err := os.WriteFile(filepath.Join(incoming, ".ready"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := lockData(dir, ".server.lock")
	if err != nil {
		t.Fatal(err)
	}
	lock, err = activateIncoming(t.Context(), dir, lock)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if data, _ := os.ReadFile(filepath.Join(dir, "marker")); string(data) != "moved" {
		t.Fatal("the committed move didn't take over")
	}
	for _, leftover := range []string{dir + ".incoming", dir + ".replaced", filepath.Join(dir, ".ready")} {
		if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s is still there", leftover)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, relocated)); err != nil {
		t.Fatal("peers won't hear the new address")
	}
}

func TestMoveGuardKeepsServerDown(t *testing.T) {
	dir := t.TempDir()
	if err := moveGuard(dir); err != nil {
		t.Fatal(err)
	}
	guard, err := lockData(dir, moveLock)
	if err != nil {
		t.Fatal(err)
	}
	if err := moveGuard(dir); err == nil {
		t.Fatal("a server started while its data was moving")
	}
	guard.Close()
	if err := os.WriteFile(filepath.Join(dir, moveUnknown), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveGuard(dir); err == nil {
		t.Fatal("a server started after a move that may have finished")
	}
}
