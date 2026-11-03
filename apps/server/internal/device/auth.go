package device

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Require protects API routes, including streams and upgraded connections.
func (b *Broker) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/network/pair" {
			b.pair(w, r)
			return
		}
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "device unauthorized", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		guard := &responseGuard{ResponseWriter: w, cancel: cancel}
		b.store.access.Lock()
		pair, err := b.store.Authenticate(ctx, token)
		if err != nil {
			b.store.access.Unlock()
			http.Error(w, "device unauthorized", http.StatusUnauthorized)
			return
		}
		if !peerRoute(pair, r.URL.Path) {
			b.store.access.Unlock()
			http.Error(w, "route unavailable to this device", http.StatusForbidden)
			return
		}
		if b.store.requests == nil {
			b.store.requests = make(map[*responseGuard]string)
		}
		b.store.requests[guard] = pair.ID
		b.store.access.Unlock()
		defer func() {
			b.store.access.Lock()
			delete(b.store.requests, guard)
			b.store.access.Unlock()
		}()
		go b.store.watch(ctx, guard, pair.ID)
		next.ServeHTTP(guard, r.WithContext(context.WithValue(ctx, selfKey{}, pair.ID)))
	})
}

// Store-owned timing keeps one test/server's streams independent of another's.
func (s *Store) watch(ctx context.Context, guard *responseGuard, id string) {
	ticker := time.NewTicker(s.recheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var revoked bool
			err := s.db.QueryRowContext(ctx, `SELECT revoked FROM devices WHERE id=?`, id).Scan(&revoked)
			if errors.Is(err, sql.ErrNoRows) || err == nil && revoked {
				guard.revoke()
				return
			}
		}
	}
}

type selfKey struct{}

// Self is the ID of the device that made the request, or empty without authentication.
func Self(ctx context.Context) string {
	id, _ := ctx.Value(selfKey{}).(string)
	return id
}

type responseGuard struct {
	http.ResponseWriter
	mu      sync.Mutex
	conn    net.Conn
	cancel  context.CancelFunc
	revoked bool
}

func (w *responseGuard) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseGuard) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *responseGuard) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	conn, buffer, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.conn = conn
		if w.revoked {
			conn.Close()
		}
	}
	return conn, buffer, err
}
func (w *responseGuard) revoke() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.revoked = true
	w.cancel()
	if w.conn != nil {
		w.conn.Close()
	}
}
