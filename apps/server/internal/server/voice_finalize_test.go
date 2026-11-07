package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
)

type finalFactory struct {
	started chan struct{}
	wait    bool
}

func (f *finalFactory) Open(ctx context.Context, _ agentruntime.Definition, _ string) (agentruntime.Session, error) {
	close(f.started)
	if f.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, errors.New("session could not open")
}

func TestVoiceOpenFinalizes(t *testing.T) {
	for _, cancelOpen := range []bool{false, true} {
		name, want := "failure", "failed"
		if cancelOpen {
			name, want = "cancellation", "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			factory := &finalFactory{started: make(chan struct{}), wait: cancelOpen}
			api, _ := voiceTest(t, &voiceFactory{}, func(config *Config) { config.Factory = factory })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/threads/a/voice",
				strings.NewReader(`{"id":"open-finalization","text":"A private request"}`))
			recorder := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				api.ServeHTTP(recorder, request)
			}()
			select {
			case <-factory.started:
			case <-time.After(3 * time.Second):
				t.Fatal("private session did not begin opening")
			}
			if cancelOpen {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("private session did not finish")
			}
			if recorder.Code != http.StatusBadGateway {
				t.Fatal("session failure was not reported", recorder.Code)
			}
			var status string
			if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT status FROM runs WHERE thread_id='a'`).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != want {
				t.Fatalf("private run remained %q, want %q", status, want)
			}
			privateChat(t, api, "a")
		})
	}
}
