package device

import (
	"context"
	"encoding/json"
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

func (b *Broker) Rename(ctx context.Context, id, name string) error {
	return b.store.Rename(ctx, id, name)
}

func (b *Broker) Revoke(ctx context.Context, id string) error {
	return b.store.Revoke(ctx, id)
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
