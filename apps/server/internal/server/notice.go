package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type PushSender interface {
	Send(context.Context, store.Push) store.PushResult
}

func (s *Server) notices(w http.ResponseWriter, r *http.Request) {
	var after *int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			fail(w, 400, "cursor", "Use a nonnegative notification cursor.")
			return
		}
		after = &n
	}
	page, err := s.Store.Notices(r.Context(), after)
	s.respond(w, page, err)
}
func (s *Server) registerPush(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		fail(w, 403, "origin", "Use the native client.")
		return
	}
	var device contract.PushDevice
	if !decode(w, r, &device) {
		return
	}
	device.Enabled = device.Enabled && s.config.Push != nil
	err := s.Store.RegisterPush(r.Context(), device)
	if errors.Is(err, store.ErrDevice) {
		fail(w, 400, "device", "Invalid push registration.")
		return
	}
	if err == nil {
		err = s.Store.BindPush(r.Context(), device.ID, clientID(r.Context()))
	}
	s.respond(w, map[string]bool{"registered": err == nil, "available": s.config.Push != nil}, err)
}
func (s *Server) sendNotices(ctx context.Context) {
	defer s.workers.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if s.admission.start() {
			for n := 0; n < 32 && ctx.Err() == nil; n++ {
				item, err := s.Store.ClaimPush(ctx)
				if errors.Is(err, sql.ErrNoRows) {
					break
				}
				if err != nil {
					if ctx.Err() == nil {
						s.config.Log.Error("claim push notification")
					}
					break
				}
				// A revoked device, possibly revoked by another process, gets no more previews.
				if s.config.Devices != nil && !s.config.Devices.Paired(ctx, item.Pairing) {
					err := errors.Join(s.Store.FinishPush(ctx, item, store.PushResult{State: "rejected"}), s.Store.RevokePush(ctx, item.Pairing))
					if err != nil {
						s.config.Log.Error("cancel revoked push notifications")
					}
					continue
				}
				result := s.config.Push.Send(ctx, item)
				writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err = s.Store.FinishPush(writeCtx, item, result)
				cancel()
				if err != nil {
					s.config.Log.Error("save push notification outcome")
				}
			}
			s.admission.done()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
