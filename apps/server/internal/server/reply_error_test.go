package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type failureFactory struct{ err error }

func (f failureFactory) Open(context.Context, agentruntime.Definition, string) (agentruntime.Session, error) {
	return &failureSession{err: f.err}, nil
}

type failureSession struct {
	testSession
	err error
}

func (s *failureSession) Send(context.Context, contract.Message, func(agentruntime.Delta) error) (agentruntime.Result, error) {
	return agentruntime.Result{}, s.err
}

func failedReply(t *testing.T, cause error, code, message string) (string, string) {
	t.Helper()
	api, endpoint := startTest(t, failureFactory{cause})
	path := filepath.Join(t.TempDir(), "reply.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	api.config.Log = slog.New(slog.NewTextHandler(file, nil))
	response, err := http.Post(endpoint.URL+"/threads/assistant/messages", "application/json", strings.NewReader(`{"id":"failure","text":"hello","attachmentIDs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d", response.StatusCode)
	}
	id := store.NewRun("failure", "assistant", "assistant", "message").ID
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := api.Store.Run(t.Context(), id)
		if err == nil && store.Terminal(run.Status) {
			if run.Error == nil || run.Error.Code != code || run.Error.Message != message {
				t.Fatalf("run error: %+v", run.Error)
			}
			if err := api.Close(); err != nil {
				t.Fatal(err)
			}
			log, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(log), "run_id="+id) || !strings.Contains(string(log), "agent=assistant") || !strings.Contains(string(log), "code="+code) {
				t.Fatalf("missing failure log: %s", log)
			}
			return run.Error.Detail, string(log)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("reply did not finish")
	return "", ""
}

func TestReplyFailureKeepsDetail(t *testing.T) {
	detail, log := failedReply(t, errors.New("dial tcp 127.0.0.1:8790: connection refused"), "provider", "The agent could not complete this reply.")
	if !strings.Contains(detail, "connection refused") || !strings.Contains(log, "connection refused") {
		t.Fatalf("lost cause: %q %q", detail, log)
	}
}

func TestReplyFailureDetailRedacted(t *testing.T) {
	cause := `Authorization: Bearer abc123secret sk-live-abc123 api_key=abc123 apikey:abc123 key=abc123 token=abc123 Bearer abc123 {"api_key":"abc123"}`
	detail, log := failedReply(t, errors.New(cause), "provider", "The agent could not complete this reply.")
	if detail == "" || !strings.Contains(detail, "[redacted]") || strings.Contains(detail+log, "abc123") {
		t.Fatalf("redaction failed: %q %q", detail, log)
	}
}

func TestReplyFailureLimit(t *testing.T) {
	detail, _ := failedReply(t, errors.New(strings.Repeat("é", 600)), "provider", "The agent could not complete this reply.")
	if len(detail) > 1024 || len(detail) == 0 {
		t.Fatalf("detail length %d", len(detail))
	}
}

type emptyFactory struct{}

func (emptyFactory) Open(context.Context, agentruntime.Definition, string) (agentruntime.Session, error) {
	return &emptySession{}, nil
}

type emptySession struct{ testSession }

func (*emptySession) Send(context.Context, contract.Message, func(agentruntime.Delta) error) (agentruntime.Result, error) {
	return agentruntime.Result{}, nil
}

func TestEmptyReplyIsFailure(t *testing.T) {
	api, endpoint := startTest(t, emptyFactory{})
	response, err := http.Post(endpoint.URL+"/threads/assistant/messages", "application/json", strings.NewReader(`{"id":"empty","text":"hello","attachmentIDs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d", response.StatusCode)
	}
	id := store.NewRun("empty", "assistant", "assistant", "message").ID
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := api.Store.Run(t.Context(), id)
		if err == nil && store.Terminal(run.Status) {
			if run.Status != "failed" || run.Error == nil || run.Error.Code != "empty" || run.Error.Message != "The agent returned an empty reply." {
				t.Fatalf("run = %+v, error = %+v", run, run.Error)
			}
			messages, err := api.Store.Messages(t.Context(), "assistant", 0, 20)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, m := range messages {
				if m.Role == "assistant" && m.Status == "error" && len(m.Parts) == 0 {
					found = true
				}
			}
			if !found {
				t.Fatalf("empty reply was not surfaced as an error: %+v", messages)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("reply did not finish")
}

func TestReplyFailureCodes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		message string
	}{
		{"stopped", context.Canceled, "Reply stopped."},
		{"timeout", context.DeadlineExceeded, "The model took too long to reply."},
	} {
		t.Run(tc.name, func(t *testing.T) { failedReply(t, tc.err, tc.name, tc.message) })
	}
}
