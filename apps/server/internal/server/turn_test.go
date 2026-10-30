package server

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestFailure(t *testing.T) {
	for _, test := range []struct {
		name        string
		err         error
		interrupted bool
		attempt     int64
		background  bool
		code        string
		status      string
		summary     string
	}{
		{name: "provider", err: errors.New("boom"), attempt: 1, code: "provider", status: contract.RunStatusFailed, summary: "Reply failed"},
		{name: "all providers unavailable", err: errors.New("provider: no providers are available right now"), attempt: 1, code: "providers_unavailable", status: contract.RunStatusFailed, summary: "Providers unavailable"},
		{name: "empty", err: errEmptyReply, attempt: 1, code: "empty", status: contract.RunStatusFailed, summary: "Empty reply"},
		{name: "stopped", err: context.Canceled, attempt: 1, code: "stopped", status: contract.RunStatusCancelled, summary: "Reply stopped"},
		{name: "membership", err: fmt.Errorf("send: %w", store.ErrMembers), attempt: 1, code: "membership", status: contract.RunStatusCancelled, summary: "Member removed"},
		{name: "workspace", err: &agentruntime.RepoError{Err: errors.New("missing")}, attempt: 1, code: "workspace", status: contract.RunStatusFailed, summary: "Repository unavailable"},
		{name: "shutdown beats cancel", err: context.Canceled, interrupted: true, attempt: 1, code: "interrupted", status: contract.RunStatusInterrupted, summary: "Execution interrupted"},
		{name: "timeout", err: context.DeadlineExceeded, attempt: 1, code: "timeout", status: contract.RunStatusFailed, summary: "Reply failed"},
		{name: "background timeout", err: context.DeadlineExceeded, attempt: 1, background: true, code: "timeout", status: contract.RunStatusFailed, summary: "Background work timed out"},
	} {
		t.Run(test.name, func(t *testing.T) {
			code, message, status, summary := failure(test.err, test.interrupted, test.attempt, test.background)
			if code != test.code || status != test.status || summary != test.summary || message == "" {
				t.Fatalf("failure() = %q, %q, %q, %q; want %q, %q, %q", code, message, status, summary, test.code, test.status, test.summary)
			}
		})
	}
	if _, message, _, _ := failure(context.Canceled, true, 2, false); message != "The server restarted again during resume. Review before retrying." {
		t.Fatalf("resumed interruption message = %q", message)
	}
}
