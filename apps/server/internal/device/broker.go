package device

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

type Broker struct {
	store *Store
	limit Limiter
}

func New(s *Store) *Broker { return &Broker{store: s} }

func (b *Broker) Issue(ctx context.Context, name string) (string, error) {
	return b.store.IssueAppToken(ctx, name)
}

func (b *Broker) Invite(ctx context.Context) (string, error) { return b.store.IssueInvite(ctx) }

func (b *Broker) pair(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Origin") != "" || r.Header.Get("X-Polymux-Client") != "polymux-kit" || !b.limit.Allow(Caller(r), time.Now()) {
		Reject(w)
		return
	}
	var input struct{ Code, Token string }
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		Reject(w)
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		Reject(w)
		return
	}
	if err := b.store.RedeemInvite(r.Context(), input.Code, input.Token); err != nil {
		status, code := http.StatusConflict, "pair_used"
		if errors.Is(err, ErrExpired) {
			status, code = http.StatusGone, "pair_expired"
		} else if !errors.Is(err, ErrUsed) {
			status, code = http.StatusInternalServerError, "pair_failed"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": "Create a new pairing link in Connections."})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (b *Broker) Rename(ctx context.Context, id, name string) error {
	return b.store.Rename(ctx, id, name)
}

func (b *Broker) Relabel(ctx context.Context, id, label string) error {
	return b.store.Relabel(ctx, id, label)
}

func (b *Broker) Revoke(ctx context.Context, id string) error {
	return b.store.Revoke(ctx, id)
}

// Paired reports whether a device is still paired.
func (b *Broker) Paired(ctx context.Context, id string) bool {
	var revoked bool
	err := b.store.db.QueryRowContext(ctx, `SELECT revoked FROM devices WHERE id=?`, id).Scan(&revoked)
	return err == nil && !revoked
}

func (b *Broker) Host(ctx context.Context, dir string) string {
	return b.store.Host(ctx, dir)
}

// All lists every device that can still be used, including other apps.
func (b *Broker) All(ctx context.Context) ([]Pairing, error) {
	pairs, err := b.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []Pairing{}
	for _, p := range pairs {
		if !p.Revoked {
			out = append(out, p)
		}
	}
	return out, nil
}

// ServeHTTP admits app enrollment, and only that, on the pinned TLS listener.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !b.limit.Allow(Caller(r), time.Now()) {
		Reject(w)
		return
	}
	// Browser origins are never admitted: only a pinned app reaches this listener.
	if r.TLS == nil || r.Header.Get("Origin") != "" {
		Reject(w)
		return
	}
	if r.Method != "POST" || r.URL.Path != "/redeem" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		Reject(w)
		return
	}
	p, err := b.store.enrollApp(r.Context(), Caller(r), input.Code)
	if err != nil {
		Reject(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(p)
}
