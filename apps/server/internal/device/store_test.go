package device

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), t.TempDir(), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestIdentity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(t.Context(), dir, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	first := fingerprint(t, appCode(t, s, []string{"https://mac.example:7339"}))
	s.Close()
	s, err = Open(t.Context(), dir, "elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if second := fingerprint(t, appCode(t, s, []string{"https://mac.example:7339"})); first != second {
		t.Fatal("server identity changed")
	}
	for _, path := range []string{filepath.Join(dir, "devices"), filepath.Join(dir, "devices", "pairing.db")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatal("pairing identity is not owner-only")
		}
	}
}

func fingerprint(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("fp")
}

func TestCertificates(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	first, err := s.certificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Leaf.NotAfter.Sub(first.Leaf.NotBefore) > 91*24*time.Hour {
		t.Fatal("leaf exceeds Apple TLS validity limit")
	}
	roots := x509.NewCertPool()
	roots.AddCert(s.issuer)
	if _, err := first.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.cert = tls.Certificate{}
	s.mu.Unlock()
	second, err := s.certificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Leaf.SerialNumber.Cmp(second.Leaf.SerialNumber) == 0 {
		t.Fatal("leaf was not renewed")
	}
	if string(first.Certificate[1]) != string(second.Certificate[1]) {
		t.Fatal("renewal changed the paired identity")
	}
	if _, err := second.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			cert, err := s.certificate(nil)
			if err != nil || cert.Leaf.SerialNumber.Cmp(second.Leaf.SerialNumber) != 0 {
				t.Error("unstable concurrent certificate generation")
			}
		})
	}
	wg.Wait()
}

func appCode(t *testing.T, s *Store, urls []string) string {
	t.Helper()
	uri, err := s.IssueApp(t.Context(), urls, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(uri)
	if err != nil || len(u.Query().Get("c")) != 43 {
		t.Fatal(uri)
	}
	return uri
}

func TestConcurrentRedeemApp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(t.Context(), dir, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	other, err := Open(t.Context(), dir, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	urls := []string{"https://mac.example:7339"}
	first := appCode(t, s, urls)
	code := codeOf(t, appCode(t, s, urls))
	if _, err := s.RedeemApp(t.Context(), "peer", codeOf(t, first)); !errors.Is(err, ErrUsed) {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, handle := range []*Store{s, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := handle.RedeemApp(context.Background(), "peer", code); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("winners = %d", wins.Load())
	}
}

func TestCodeExpiresAt3Min(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	now := time.Unix(1_700_000_000, 0)
	s.clock = func() time.Time { return now }
	urls := []string{"https://mac.example:7339"}
	uri := appCode(t, s, urls)
	now = now.Add(3*time.Minute - time.Second)
	if _, err := s.RedeemApp(t.Context(), "peer", codeOf(t, uri)); err != nil {
		t.Fatal(err)
	}
	uri = appCode(t, s, urls)
	now = now.Add(3 * time.Minute)
	if _, err := s.RedeemApp(t.Context(), "peer", codeOf(t, uri)); !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
}

func TestQRPayloadNoLoopback(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	uri := appCode(t, s, []string{"https://127.0.0.1:7339", "https://[::1]:7339", "https://localhost:7339", "https://mac.example:7339"})
	q, _ := url.Parse(uri)
	sum := sha256.Sum256(s.issuer.Raw)
	params := q.Query()
	if params.Get("v") != "2" || params.Get("fp") != hex.EncodeToString(sum[:]) || len(params["u"]) != 1 || !strings.Contains(params["u"][0], "mac.example") {
		t.Fatal(uri)
	}
	if strings.Contains(uri, "127.0.0.1") || strings.Contains(uri, "localhost") || strings.Contains(uri, "::1") || strings.Contains(uri, "%3A%3A1") {
		t.Fatal(uri)
	}
}

func TestLockoutPerCaller(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	open := func() *Store {
		t.Helper()
		s, err := Open(t.Context(), dir, "127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		s.clock = func() time.Time { return now }
		return s
	}
	s := open()
	urls := []string{"https://mac.example:7339"}
	code := codeOf(t, appCode(t, s, urls))
	bad := strings.Repeat("b", 43)
	for range 3 {
		if _, err := s.RedeemApp(t.Context(), "a", bad); !errors.Is(err, ErrUsed) {
			t.Fatal(err)
		}
	}
	s.Close()
	s = open()
	for range 2 {
		if _, err := s.RedeemApp(t.Context(), "a", bad); !errors.Is(err, ErrUsed) {
			t.Fatal(err)
		}
	}
	if _, err := s.RedeemApp(t.Context(), "a", code); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	if agent, err := s.RedeemApp(t.Context(), "b", code); err != nil || agent != "assistant" {
		t.Fatalf("other caller: %v", err)
	}
	now = now.Add(15*time.Minute + time.Second)
	s.Close()
	s = open()
	defer s.Close()
	if agent, err := s.RedeemApp(t.Context(), "a", codeOf(t, appCode(t, s, urls))); err != nil || agent != "assistant" {
		t.Fatal(err)
	}
}

func TestLinkHasNoPolymuxHost(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	if _, err := s.IssueApp(t.Context(), nil, "assistant"); !errors.Is(err, ErrNoAddress) {
		t.Fatal(err)
	}
	if _, err := s.IssueApp(t.Context(), []string{"https://connect.polymux.com"}, "assistant"); !errors.Is(err, ErrNoAddress) {
		t.Fatal(err)
	}
	uri := appCode(t, s, []string{"https://connect.polymux.com", "https://mac.example:7339"})
	if strings.Contains(strings.ToLower(uri), "polymux.com") {
		t.Fatal(uri)
	}
}

func TestIdentityRejectsUnspecified(t *testing.T) {
	t.Parallel()
	if _, err := Open(t.Context(), t.TempDir(), "0.0.0.0"); err == nil {
		t.Fatal("accepted unspecified host")
	}
}

func codeOf(t *testing.T, uri string) string {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("c")
}

