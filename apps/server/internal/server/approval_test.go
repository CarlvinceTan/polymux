package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestStableJSONKeyOrder(t *testing.T) {
	a, err := stableJSON(json.RawMessage(`{"b":1,"a":{"d":2,"c":[3,{"z":1,"y":2}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := stableJSON(json.RawMessage(`{"a":{"c":[3,{"y":2,"z":1}],"d":2},"b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("canonical mismatch:\n%s\n%s", a, b)
	}
	key1, err := approvalKey("run-1", "write", json.RawMessage(`{"b":1,"a":2}`))
	if err != nil {
		t.Fatal(err)
	}
	key2, err := approvalKey("run-1", "write", json.RawMessage(`{"a":2,"b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if key1 != key2 {
		t.Fatalf("keys differ: %s vs %s", key1, key2)
	}
}

func TestClassifyToolTableAndHeuristics(t *testing.T) {
	cases := []struct {
		name     string
		declared ToolClass
		known    bool
		external bool
		want     ToolClass
	}{
		{"read", "", true, true, ToolRead},
		{"track_work", "", true, false, ToolReversible},
		{"bash", "", true, true, ToolConsequential},
		{"message_agent", "", true, false, ToolReversible},
		{"handoff_agent", "", true, false, ToolReversible},
		{"manage_routines", "", true, false, ToolConsequential},
		{"consequential_delete", ToolConsequential, true, true, ToolConsequential},
		{"send_email", "", false, true, ToolConsequential},
		{"mystery_mcp", "", false, true, ToolConsequential},
		{"side_effect", "", true, true, ToolConsequential},
		{"side_effect", ToolReversible, true, true, ToolReversible},
		{"show_html", "", true, false, ToolRead},
		{"send_to_user", "", true, false, ToolReversible},
	}
	for _, tc := range cases {
		if got := classifyTool(tc.name, tc.declared, tc.known, tc.external); got != tc.want {
			t.Fatalf("%s declared=%q: got %s want %s", tc.name, tc.declared, got, tc.want)
		}
	}
}

func TestReviewedMCPReadsDoNotNeedApproval(t *testing.T) {
	for _, reviewed := range []bool{false, true} {
		t.Run(fmt.Sprint(reviewed), func(t *testing.T) {
			db := openApprovalStore(t)
			run := seedApprovalRun(t, db, "mcp-read")
			state := &turnState{run: run, store: db, prefixes: []string{"safe__"}}
			if reviewed {
				state.readPrefixes = []string{"safe__"}
			}
			decision := gateApproval(t.Context(), state, "call", "safe__mail_accounts", json.RawMessage(`{}`))
			if (decision == nil) != reviewed {
				t.Fatal("read approval did not follow the operator-reviewed connection")
			}
			if decision := gateApproval(t.Context(), state, "unknown", "other__mail_accounts", json.RawMessage(`{}`)); decision == nil {
				t.Fatal("unclassified MCP connection bypassed approval")
			}
		})
	}
}

func TestApprovalExactlyOnce(t *testing.T) {
	db := openApprovalStore(t)
	run := seedApprovalRun(t, db, "job-once")
	args := json.RawMessage(`{"target":"x"}`)
	key, err := approvalKey(run.ID, "consequential_delete", args)
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := db.CreateApproval(t.Context(), store.Approval{
		ID: "approval-once", RunID: run.ID, AgentID: "agent", ThreadID: "agent",
		Tool: "consequential_delete", Args: args, Key: key, JobID: run.JobID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DecideApproval(t.Context(), created.ID, store.ApprovalApproved); err != nil {
		t.Fatal(err)
	}
	again, err := db.DecideApproval(t.Context(), created.ID, store.ApprovalApproved)
	if err != nil || again.Status != store.ApprovalApproved {
		t.Fatalf("double approve: %+v %v", again, err)
	}

	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			state := &turnState{
				run: run, store: db, classes: map[string]ToolClass{"consequential_delete": ToolConsequential},
				known: map[string]bool{"consequential_delete": true}, external: map[string]bool{"consequential_delete": true},
			}
			ctx := context.WithValue(t.Context(), turnKey{}, state)
			result := beforeTool(ctx, fmt.Sprintf("c-race-%d", i), "consequential_delete", args)
			if !result.Block {
				allowed.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if allowed.Load() != 1 {
		t.Fatalf("concurrent claim allowed %d executions, want 1", allowed.Load())
	}

	state := &turnState{
		run: run, store: db, classes: map[string]ToolClass{"consequential_delete": ToolConsequential},
		known: map[string]bool{"consequential_delete": true}, external: map[string]bool{"consequential_delete": true},
	}
	second := beforeTool(context.WithValue(t.Context(), turnKey{}, state), "c-replay", "consequential_delete", args)
	if !second.Block || !strings.Contains(second.Reason, "unavailable") && !strings.Contains(second.Reason, "already executed") {
		t.Fatalf("replay not blocked: %+v", second)
	}
}

func TestApprovalRestartReplay(t *testing.T) {
	db := openApprovalStore(t)
	run := seedApprovalRun(t, db, "job-restart")
	args := json.RawMessage(`{"target":"y"}`)
	key, err := approvalKey(run.ID, "consequential_delete", args)
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := db.CreateApproval(t.Context(), store.Approval{
		ID: "approval-restart", RunID: run.ID, AgentID: "agent", ThreadID: "agent",
		Tool: "consequential_delete", Args: args, Key: key, JobID: run.JobID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DecideApproval(t.Context(), created.ID, store.ApprovalApproved); err != nil {
		t.Fatal(err)
	}
	state := &turnState{
		run: run, store: db, classes: map[string]ToolClass{"consequential_delete": ToolConsequential},
		known: map[string]bool{"consequential_delete": true}, external: map[string]bool{"consequential_delete": true},
	}
	if result := beforeTool(context.WithValue(t.Context(), turnKey{}, state), "c1", "consequential_delete", args); result.Block {
		t.Fatalf("approved call blocked: %+v", result)
	}
	if err := db.SaveApprovalResult(t.Context(), created.ID, json.RawMessage(`{"status":"completed"}`)); err != nil {
		t.Fatal(err)
	}
	// Simulate process restart: fresh turn state against the same store.
	restarted := &turnState{
		run: run, store: db, classes: map[string]ToolClass{"consequential_delete": ToolConsequential},
		known: map[string]bool{"consequential_delete": true}, external: map[string]bool{"consequential_delete": true},
	}
	replay := beforeTool(context.WithValue(t.Context(), turnKey{}, restarted), "c1-retry", "consequential_delete", args)
	if !replay.Block || !strings.Contains(replay.Reason, "already executed") {
		t.Fatalf("restart re-issue not blocked: %+v", replay)
	}
}

func TestApprovalDenyFinal(t *testing.T) {
	db := openApprovalStore(t)
	run := seedApprovalRun(t, db, "job-deny")
	args := json.RawMessage(`{"target":"x"}`)
	key, _ := approvalKey(run.ID, "consequential_delete", args)
	created, _, err := db.CreateApproval(t.Context(), store.Approval{
		ID: "approval-deny", RunID: run.ID, AgentID: "agent", ThreadID: "agent",
		Tool: "consequential_delete", Args: args, Key: key, JobID: run.JobID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DecideApproval(t.Context(), created.ID, store.ApprovalDenied); err != nil {
		t.Fatal(err)
	}
	state := &turnState{
		run: run, store: db, classes: map[string]ToolClass{"consequential_delete": ToolConsequential},
		known: map[string]bool{"consequential_delete": true}, external: map[string]bool{"consequential_delete": true},
	}
	result := beforeTool(context.WithValue(t.Context(), turnKey{}, state), "c1", "consequential_delete", args)
	if !result.Block || result.Reason != denialMessage("consequential_delete") {
		t.Fatalf("deny block: %+v", result)
	}
	pending, err := db.PendingApprovals(t.Context(), run.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("retry created approval: %v %+v", err, pending)
	}
}

func TestApprovalExpiry(t *testing.T) {
	db := openApprovalStore(t)
	run := seedApprovalRun(t, db, "job-exp")
	args := json.RawMessage(`{"target":"x"}`)
	key, _ := approvalKey(run.ID, "consequential_delete", args)
	created, _, err := db.CreateApproval(t.Context(), store.Approval{
		ID: "approval-exp", RunID: run.ID, AgentID: "agent", ThreadID: "agent",
		Tool: "consequential_delete", Args: args, Key: key, JobID: run.JobID,
		ExpiresAt: "2000-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DecideApproval(t.Context(), created.ID, store.ApprovalApproved); err == nil {
		t.Fatal("approve after expiry should fail")
	}
	got, err := db.Approval(t.Context(), created.ID)
	if err != nil || got.Status != store.ApprovalExpired {
		t.Fatalf("expired status: %+v %v", got, err)
	}
	state := &turnState{
		run: run, store: db, classes: map[string]ToolClass{"consequential_delete": ToolConsequential},
		known: map[string]bool{"consequential_delete": true}, external: map[string]bool{"consequential_delete": true},
	}
	result := beforeTool(context.WithValue(t.Context(), turnKey{}, state), "c1", "consequential_delete", args)
	if !result.Block || result.Reason != expiredMessage("consequential_delete") {
		t.Fatalf("expired gate: %+v", result)
	}
}

func TestAutomaticAllowlistBeforeApproval(t *testing.T) {
	// manage_routines is explicitly forbidden on automatic turns; approval must not be created.
	db := openApprovalStore(t)
	run := seedApprovalRun(t, db, "job-auto")
	run.Trigger = "routine"
	state := &turnState{
		run:     run,
		work:    store.Work{Trigger: "routine"},
		store:   db,
		classes: map[string]ToolClass{"manage_routines": ToolConsequential},
		known:   map[string]bool{"manage_routines": true},
	}
	result := beforeTool(context.WithValue(t.Context(), turnKey{}, state), "c1", "manage_routines", json.RawMessage(`{}`))
	if !result.Block || result.Reason != "Automatic turns cannot use external or scheduling tools" {
		t.Fatalf("allowlist should win: %+v", result)
	}
	if state.approval != nil {
		t.Fatal("approval should not be created when allowlist blocks")
	}
	pending, err := db.PendingApprovals(t.Context(), run.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("allowlist leak created approval: %v %+v", err, pending)
	}
}

func openApprovalStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Register(t.Context(), []contract.Agent{{ID: "agent", Name: "Agent"}}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedApprovalRun(t *testing.T, db *store.Store, job string) contract.Run {
	t.Helper()
	if _, err := db.DB.ExecContext(t.Context(), `INSERT INTO jobs(id,thread_id,request,state,input) VALUES(?,?,'{}','done','{}')`, job, "agent"); err != nil {
		t.Fatal(err)
	}
	run := store.NewRun(job, "agent", "agent", "message")
	run.Status = "running"
	if err := db.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
		t.Fatal(err)
	}
	return run
}
