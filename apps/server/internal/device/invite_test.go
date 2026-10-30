package device

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInviteSingleDevice(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir(), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	code, err := s.IssueInvite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(t.Context(), code); err == nil {
		t.Fatal("invitation is an API credential")
	}
	first, second := randomID(), randomID()
	if err := s.RedeemInvite(t.Context(), code, first); err != nil {
		t.Fatal(err)
	}
	device, err := s.Authenticate(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RedeemInvite(t.Context(), code, first); err != nil {
		t.Fatal("lost-response retry failed:", err)
	}
	if err := s.RedeemInvite(t.Context(), code, second); !errors.Is(err, ErrUsed) {
		t.Fatal("another device reused the invitation:", err)
	}
	if err := s.RedeemInvite(t.Context(), code, code); !errors.Is(err, ErrUsed) {
		t.Fatal("invitation became a bearer:", err)
	}
	if _, err := s.Authenticate(t.Context(), second); err == nil {
		t.Fatal("refused credential was installed")
	}
	fresh, _ := s.IssueInvite(t.Context())
	if err := s.RedeemInvite(t.Context(), fresh, second); err != nil {
		t.Fatal(err)
	}
	other, err := s.Authenticate(t.Context(), second)
	if err != nil || other.ID == device.ID {
		t.Fatal("fresh invitation did not create a separate identity")
	}
	if err := s.Revoke(t.Context(), device.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RedeemInvite(t.Context(), code, first); !errors.Is(err, ErrUsed) {
		t.Fatal("retry resurrected a revoked device:", err)
	}
}

func TestInviteConcurrentClaims(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir(), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	code, _ := s.IssueInvite(t.Context())
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			err := s.RedeemInvite(t.Context(), code, randomID())
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, ErrUsed) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("accepted %d independent claims", wins.Load())
	}
	devices, err := s.List(t.Context())
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices=%d err=%v", len(devices), err)
	}
}

func TestInviteRollbackAndExpiry(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir(), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	s.clock = func() time.Time { return now }
	code, _ := s.IssueInvite(t.Context())
	token := randomID()
	if _, err := s.db.Exec(`CREATE TRIGGER fail_invite BEFORE INSERT ON devices BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.RedeemInvite(t.Context(), code, token); err == nil {
		t.Fatal("fixture did not reject device insertion")
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_invite`); err != nil {
		t.Fatal(err)
	}
	if err := s.RedeemInvite(t.Context(), code, token); err != nil {
		t.Fatal("failed insertion consumed the invitation:", err)
	}
	expiring, _ := s.IssueInvite(t.Context())
	now = now.Add(InviteTTL)
	if err := s.RedeemInvite(t.Context(), expiring, randomID()); !errors.Is(err, ErrExpired) {
		t.Fatal("expired invitation accepted:", err)
	}
	if _, err := s.IssueInvite(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM invitations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expired invitations remain: %d %v", count, err)
	}
}
