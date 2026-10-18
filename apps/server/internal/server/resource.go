package server

import "context"

type execution struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *Server) acquire(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resource, _ := s.resources.LoadOrStore(id, make(chan struct{}, 1))
	gate := resource.(chan struct{})
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case s.slots <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-s.slots
			<-gate
			return nil, err
		}
		return func() { <-s.slots; <-gate }, nil
	case <-ctx.Done():
		<-gate
		return nil, ctx.Err()
	}
}
