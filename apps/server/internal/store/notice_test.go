package store

import (
	"database/sql"
	"errors"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"strings"
	"testing"
)

func TestNoticesCommitDeduplicateAndRecoverPush(t *testing.T) {
	t.Parallel()
	s := runStore(t)
	device := contract.PushDevice{ID: "fixture-installation", Token: strings.Repeat("ab", 32), Environment: "sandbox", Server: "http://server.test:7337", Enabled: true}
	if err := s.RegisterPush(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	text := "Do not expose this message in the notification payload"
	m := contract.Message{ID: "reply", ThreadID: "a", AgentID: ptr("a"), Role: "assistant", Status: "streaming", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.start"}, &m); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	page, err := s.Notices(t.Context(), &zero)
	if err != nil || len(page.Notices) != 0 {
		t.Fatal("streaming message notified")
	}
	m.Status = "done"
	for range 2 {
		if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
	}
	page, err = s.Notices(t.Context(), &zero)
	if err != nil || len(page.Notices) != 1 || page.Cursor != page.Notices[0].Seq || page.Notices[0].Title != "Same" {
		t.Fatalf("page %+v %v", page, err)
	}
	baseline, err := s.Notices(t.Context(), nil)
	if err != nil || baseline.Cursor != page.Cursor || len(baseline.Notices) != 0 {
		t.Fatal("first connect replays historical notifications")
	}
	push, err := s.ClaimPush(t.Context())
	if err != nil || push.Device.Token != device.Token {
		t.Fatal(err)
	}
	if err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimPush(t.Context()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("ambiguous push replayed")
	}
	var state string
	if err := s.DB.QueryRowContext(t.Context(), `SELECT state FROM push_queue`).Scan(&state); err != nil || state != "unknown" {
		t.Fatalf("outcome %s %v", state, err)
	}
}

func TestFailedPushRetriesWithBackoffAndThenStops(t *testing.T) {
	s := runStore(t)
	device := contract.PushDevice{ID: "fixture-installation", Token: strings.Repeat("ab", 32), Environment: "sandbox", Server: "http://server.test:7337", Enabled: true}
	if err := s.RegisterPush(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	text := "Fixture notice"
	m := contract.Message{ID: "reply", ThreadID: "a", AgentID: ptr("a"), Role: "assistant", Status: "done", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.start"}, &m); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= PushAttempts; attempt++ {
		push, err := s.ClaimPush(t.Context())
		if err != nil {
			t.Fatalf("attempt %d not claimable: %v", attempt, err)
		}
		if err := s.FinishPush(t.Context(), push, PushResult{State: "failed"}); err != nil {
			t.Fatal(err)
		}
		if attempt < PushAttempts {
			if _, err := s.ClaimPush(t.Context()); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("attempt %d retried before its backoff", attempt)
			}
			if _, err := s.DB.ExecContext(t.Context(), `UPDATE push_queue SET retry_at=NULL`); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.ClaimPush(t.Context()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("exhausted push retried again")
	}
	var state string
	if err := s.DB.QueryRowContext(t.Context(), `SELECT state FROM push_queue`).Scan(&state); err != nil || state != "failed" {
		t.Fatalf("terminal state %q %v", state, err)
	}
}

func TestPushRetriesOnce(t *testing.T) {
	s := runStore(t)
	device := contract.PushDevice{ID: "fixture-installation", Token: strings.Repeat("ab", 32), Environment: "sandbox", Server: "http://server.test:7337", Enabled: true}
	if err := s.RegisterPush(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	text := "Fixture notice"
	m := contract.Message{ID: "once", ThreadID: "a", AgentID: ptr("a"), Role: "assistant", Status: "done", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.start"}, &m); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		push, err := s.ClaimPush(t.Context())
		if err != nil {
			t.Fatalf("retry missing: %v", err)
		}
		if err := s.FinishPush(t.Context(), push, PushResult{State: "failed"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.ExecContext(t.Context(), `UPDATE push_queue SET retry_at=NULL`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ClaimPush(t.Context()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("push retried more than once")
	}
}

func TestPruneNoticesDropsSettledRowsOnly(t *testing.T) {
	s := runStore(t)
	device := contract.PushDevice{ID: "fixture-installation", Token: strings.Repeat("ab", 32), Environment: "sandbox", Server: "http://server.test:7337", Enabled: true}
	if err := s.RegisterPush(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"old", "new"} {
		text := "Fixture notice"
		m := contract.Message{ID: id, ThreadID: "a", AgentID: ptr("a"), Role: "assistant", Status: "done", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}
		if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.start"}, &m); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PruneNotices(t.Context()); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	page, err := s.Notices(t.Context(), &zero)
	if err != nil || len(page.Notices) != 2 {
		t.Fatalf("recent feed pruned: %+v %v", page, err)
	}
	// Age one notice past retention and settle both rows.
	if _, err := s.DB.ExecContext(t.Context(), `UPDATE notices SET data=json_set(data,'$.createdAt','2000-01-01T00:00:00Z') WHERE seq=(SELECT min(seq) FROM notices)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(t.Context(), `UPDATE push_queue SET state='sent'`); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneNotices(t.Context()); err != nil {
		t.Fatal(err)
	}
	page, err = s.Notices(t.Context(), &zero)
	if err != nil || len(page.Notices) != 1 || page.Notices[0].MessageID != "new" {
		t.Fatalf("retention wrong: %+v %v", page, err)
	}
	var queued int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM push_queue`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("settled rows %d %v", queued, err)
	}
}

func TestNoticesRejectDevicesAndQuietReplies(t *testing.T) {
	t.Parallel()
	s := runStore(t)
	for _, environment := range []string{"", "unknown"} {
		if err := s.RegisterPush(t.Context(), contract.PushDevice{ID: "installation-valid", Environment: environment, Enabled: true, Token: "invalid"}); !errors.Is(err, ErrDevice) {
			t.Fatal("invalid device accepted")
		}
	}
	m := contract.Message{ID: "quiet", ThreadID: "a", Role: "assistant", Status: "done", Parts: []contract.Part{}}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	page, err := s.Notices(t.Context(), &zero)
	if err != nil || len(page.Notices) != 0 {
		t.Fatal("quiet reply notified")
	}
}

func TestNoticeOncePerTurn(t *testing.T) {
	s := runStore(t)
	run := NewRun("turn", "a", "a", "message")
	if err := s.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two", "three"} {
		text := id + "\nMore detail"
		m := contract.Message{ID: id, ThreadID: "a", AgentID: ptr("a"), RunID: &run.ID, Role: "assistant", Status: "done", Parts: []contract.Part{{Kind: "text", Text: &text}}}
		if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
	}
	zero := int64(0)
	page, err := s.Notices(t.Context(), &zero)
	if err != nil || len(page.Notices) != 0 {
		t.Fatalf("early notice: %+v %v", page, err)
	}
	run.Status = "completed"
	for range 2 {
		if err := s.RunEvent(t.Context(), "run.done", &run, nil); err != nil {
			t.Fatal(err)
		}
	}
	page, err = s.Notices(t.Context(), &zero)
	if err != nil || len(page.Notices) != 1 {
		t.Fatalf("notices: %+v %v", page, err)
	}
	if n := page.Notices[0]; n.Body != "three" || n.Outcome != "done" || n.MessageID != "three" {
		t.Fatalf("notice: %+v", n)
	}
}

func TestNoticeOnFailure(t *testing.T)      { noticeOutcome(t, "failed", "Failed: Tool crashed") }
func TestNoticeOnApprovalWait(t *testing.T) { noticeOutcome(t, "waiting_input", "Needs your approval") }

func noticeOutcome(t *testing.T, status, body string) {
	t.Helper()
	s := runStore(t)
	run := NewRun("turn", "a", "a", "message")
	if err := s.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
		t.Fatal(err)
	}
	run.Status = status
	if status == "waiting_input" {
		if _, _, err := s.CreateApproval(t.Context(), Approval{ID: "gate", RunID: run.ID, AgentID: "a", ThreadID: "a", Tool: "exec", Key: "gate"}); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := s.RunEvent(t.Context(), "run.done", &run, nil); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.DecideApproval(t.Context(), "gate", ApprovalApproved); err != nil {
			t.Fatal(err)
		}
		if _, claimed, err := s.ClaimApproval(t.Context(), "gate"); err != nil || !claimed {
			t.Fatalf("claim approval: %v %v", claimed, err)
		}
		run.Status = "completed"
	}
	run.Error = &contract.Error{Message: "Tool crashed\nDetails"}
	for range 2 {
		if err := s.RunEvent(t.Context(), "run.done", &run, nil); err != nil {
			t.Fatal(err)
		}
	}
	zero := int64(0)
	page, err := s.Notices(t.Context(), &zero)
	want := 1
	if status == "waiting_input" {
		want = 2
	}
	if err != nil || len(page.Notices) != want {
		t.Fatalf("notices: %+v %v", page, err)
	}
	if want == 2 && (page.Notices[1].Outcome != "done" || page.Notices[1].Body != "Finished") {
		t.Fatalf("finished notice: %+v", page.Notices[1])
	}
	if n := page.Notices[0]; n.Body != body || n.Outcome != status {
		t.Fatalf("notice: %+v", n)
	}
}

func TestNoticeBodyFirstLine(t *testing.T) {
	for _, body := range []string{" First line\nSecond line", strings.Repeat("界", 150) + "\nDetails"} {
		got := noticeBody(body)
		want := "First line"
		if strings.HasPrefix(body, "界") {
			want = strings.Repeat("界", 140)
		}
		if got != want {
			t.Fatalf("body %q, want %q", got, want)
		}
	}
}

func TestNoticeTriggerRules(t *testing.T) {
	for _, trigger := range []string{"routine", "peer", "created"} {
		t.Run(trigger, func(t *testing.T) {
			s := runStore(t)
			run := NewRun("turn", "a", "a", trigger)
			if err := s.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
				t.Fatal(err)
			}
			text := "Reply"
			m := contract.Message{ID: "reply", ThreadID: "a", RunID: &run.ID, Role: "assistant", Status: "done", Parts: []contract.Part{{Kind: "text", Text: &text}}}
			if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
				t.Fatal(err)
			}
			run.Status = "failed"
			if err := s.RunEvent(t.Context(), "run.done", &run, nil); err != nil {
				t.Fatal(err)
			}
			zero := int64(0)
			page, err := s.Notices(t.Context(), &zero)
			want := 0
			if trigger == "routine" {
				want = 1
			}
			if err != nil || len(page.Notices) != want {
				t.Fatalf("notices: %+v %v", page, err)
			}
		})
	}
}

func TestNoticeMigrationPendingPush(t *testing.T) {
	s := runStore(t)
	ctx := t.Context()
	var schema string
	if err := s.DB.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name='notices'`).Scan(&schema); err != nil || !strings.Contains(schema, "notice_key TEXT NOT NULL UNIQUE") || strings.Contains(schema, "message_id TEXT NOT NULL UNIQUE") {
		t.Fatalf("fresh schema: %s %v", schema, err)
	}
	text := "Reply"
	m := contract.Message{ID: "legacy", ThreadID: "a", Role: "assistant", Status: "done", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}
	if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	_, err := s.DB.ExecContext(ctx, `DROP TABLE push_queue; DROP TABLE notices;
CREATE TABLE notices(seq INTEGER PRIMARY KEY AUTOINCREMENT, thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE, message_id TEXT NOT NULL UNIQUE REFERENCES messages(id) ON DELETE CASCADE, data TEXT NOT NULL);
CREATE TABLE push_queue(notice_seq INTEGER NOT NULL REFERENCES notices(seq) ON DELETE CASCADE, device_id TEXT NOT NULL REFERENCES push_devices(id) ON DELETE CASCADE, state TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0, retry_at INTEGER, PRIMARY KEY(notice_seq,device_id));
INSERT INTO push_devices VALUES('legacy-device','token','sandbox','server',1);
INSERT INTO notices VALUES(7,'a','legacy',json_object('seq',7,'conversationID','a','messageID','legacy','title','Polymux','createdAt',strftime('%Y-%m-%dT%H:%M:%SZ','now')));
INSERT INTO push_queue VALUES(7,'legacy-device','pending',1,123);`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.migrateNotices(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var seq, attempts, retry int
	var state string
	if err := s.DB.QueryRowContext(ctx, `SELECT notice_seq,state,attempts,retry_at FROM push_queue`).Scan(&seq, &state, &attempts, &retry); err != nil || seq != 7 || state != "pending" || attempts != 1 || retry != 123 {
		t.Fatalf("queue: %d %s %d %d: %v", seq, state, attempts, retry, err)
	}
	zero := int64(0)
	page, err := s.Notices(ctx, &zero)
	if err != nil || len(page.Notices) != 1 || page.Notices[0].Body != "New message in Polymux" || page.Notices[0].Outcome != "done" {
		t.Fatalf("migrated notices: %+v %v", page, err)
	}
	var violations int
	rows, err := s.DB.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		violations++
	}
	if err := rows.Err(); err != nil || violations != 0 {
		t.Fatalf("foreign keys: %d %v", violations, err)
	}
}
