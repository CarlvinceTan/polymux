package server

import "context"

type execution struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// acquire takes the turn of one session, keyed by id. Any number of sessions run at once; a model provider that
// can't keep up slows its own calls down instead (see the runtime's providerLoad).
func (s *Server) acquire(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resource, _ := s.resources.LoadOrStore(id, make(chan struct{}, 1))
	gate := resource.(chan struct{})
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
