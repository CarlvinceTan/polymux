package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestPinPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}); err != nil {
		t.Fatal(err)
	}
	group, err := s.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}, Title: new("Saved title")})
	if err != nil {
		t.Fatal(err)
	}
	msg := contract.Message{ID: "live-message", ThreadID: group.ID, Role: "assistant", Status: "done", Parts: []contract.Part{{Kind: "text", Text: new("Saved history")}}, CreatedAt: Now()}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: group.ID, Type: "message.done", MessageID: &msg.ID}, &msg); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"b", "group", "b"} {
		if err := s.UpdateThread(t.Context(), id, contract.UpdateThreadRequest{Pinned: new(true)}); err != nil {
			t.Fatal(err)
		}
	}
	threads, err := s.Threads(t.Context())
	if err != nil || !slices.Equal([]string{threads[0].ID, threads[1].ID, threads[2].ID}, []string{"b", "group", "a"}) ||
		threads[0].PinOrder == nil || *threads[0].PinOrder != 0 || threads[1].PinOrder == nil || *threads[1].PinOrder != 1 {
		t.Fatal("pins did not retain insertion order", err)
	}
	if err := s.UpdateThread(t.Context(), "group", contract.UpdateThreadRequest{PinOrder: new(0.5)}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateThread(t.Context(), "b", contract.UpdateThreadRequest{Pinned: new(false)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	threads, err = s.Threads(t.Context())
	if err != nil || threads[0].ID != group.ID || (threads[0].Pinned == nil || !*threads[0].Pinned) || threads[0].PinOrder == nil || *threads[0].PinOrder != 0.5 || threads[0].UnreadCount != 1 {
		t.Fatal("pin order did not survive reopening", err)
	}
	after, err := s.Thread(t.Context(), group.ID)
	if err != nil || after.Title != group.Title || !reflect.DeepEqual(after.AgentIDs, group.AgentIDs) || after.LastSeq != 1 || after.LastReadSeq != 0 {
		t.Fatal("pin update changed chat metadata or history", err)
	}
	if stored, err := s.Message(t.Context(), msg.ID); err != nil || stored.ThreadID != group.ID || *stored.Parts[0].Text != "Saved history" {
		t.Fatal("pin update lost a live message", err)
	}
	b, err := s.Thread(t.Context(), "b")
	if err != nil || (b.Pinned == nil || *b.Pinned) || b.PinOrder != nil {
		t.Fatal("unpin did not clear its order", err)
	}
	var title string
	if err := s.DB.QueryRowContext(t.Context(), `SELECT title FROM threads WHERE id='b'`).Scan(&title); err != nil || title != "" {
		t.Fatal("pinning froze a direct chat's derived title", err)
	}
}

func TestPinLimit(t *testing.T) {
	s := openTest(t)
	agents := make([]contract.Agent, 12)
	for i := range agents {
		agents[i] = contract.Agent{ID: fmt.Sprintf("pin-%02d", i), Name: "Pinnable"}
	}
	if err := s.Register(t.Context(), agents); err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if err := s.UpdateThread(t.Context(), a.ID, contract.UpdateThreadRequest{Pinned: new(true)}); err != nil {
			t.Fatal(err)
		}
	}
	group, err := s.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}, Title: new("Keep title")})
	if err != nil {
		t.Fatal(err)
	}
	err = s.UpdateThread(t.Context(), group.ID, contract.UpdateThreadRequest{Pinned: new(true), Title: new("Changed"), AgentIDs: []string{"b", "a"}})
	if !errors.Is(err, ErrPinLimit) {
		t.Fatal("pin limit was not enforced", err)
	}
	after, err := s.Thread(t.Context(), group.ID)
	if err != nil || (after.Pinned == nil || *after.Pinned) || after.Title != group.Title || !reflect.DeepEqual(after.AgentIDs, group.AgentIDs) {
		t.Fatal("pin limit left a partial membership/title update", err)
	}
	if err := s.UpdateThread(t.Context(), agents[0].ID, contract.UpdateThreadRequest{Pinned: new(true)}); err != nil {
		t.Fatal("idempotent pin failed at the limit", err)
	}
}

func TestPinValidation(t *testing.T) {
	s := openTest(t)
	if err := s.UpdateThread(t.Context(), "missing", contract.UpdateThreadRequest{Pinned: new(true)}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("missing thread was not rejected", err)
	}
	for _, order := range []float64{-1, math.NaN(), math.Inf(1)} {
		if err := s.UpdateThread(t.Context(), "a", contract.UpdateThreadRequest{Pinned: new(true), PinOrder: &order}); !errors.Is(err, ErrPinOrder) {
			t.Fatal("invalid pin order accepted", err)
		}
	}
	if err := s.UpdateThread(t.Context(), "a", contract.UpdateThreadRequest{PinOrder: new(0.0)}); !errors.Is(err, ErrPinOrder) {
		t.Fatal("an unpinned thread accepted a pin order", err)
	}
	if err := s.UpdateThread(t.Context(), "a", contract.UpdateThreadRequest{Pinned: new(true), PinOrder: new(0.0)}); err != nil {
		t.Fatal("explicit source pin order was rejected", err)
	}
}

func TestPinUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "Existing agent"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(t.Context(), `ALTER TABLE threads DROP COLUMN pin_order; ALTER TABLE threads DROP COLUMN pinned`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	thread, err := s.Thread(t.Context(), "a")
	if err != nil || thread.Title != "Existing agent" || (thread.Pinned == nil || *thread.Pinned) || thread.PinOrder != nil {
		t.Fatal("existing thread was reset during the pin migration", err)
	}
}
