package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func runStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "Same"}, {ID: "b", Name: "Same"}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunRecovery(t *testing.T) {
	s := runStore(t)
	for _, id := range []string{"active", "next"} {
		req := contract.SendRequest{ID: id, Text: "Hello", AttachmentIDs: []string{}}
		if _, _, err := s.Send(t.Context(), "a", req, []contract.Part{{Kind: "text", Text: &req.Text}}, "a"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Job(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	run, err := s.Run(t.Context(), NewRun("active", "a", "a", "message").ID)
	if err != nil {
		t.Fatal(err)
	}
	run.Status, run.Activity, run.ToolID = "running", "tool", ptr("call")
	if err := s.RunEvent(t.Context(), "tool.start", &run, &contract.ToolActivity{ID: "call", RunID: run.ID, Name: "write", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	run, err = s.Run(t.Context(), run.ID)
	if err != nil || run.Status != "interrupted" || run.Activity != "" || run.Error == nil {
		t.Fatalf("recovery %+v %v", run, err)
	}
	next, err := s.Job(t.Context(), "a")
	if err != nil || next != "next" {
		t.Fatalf("queued work lost %s %v", next, err)
	}
	thread, err := s.Thread(t.Context(), "a")
	if err != nil || len(thread.Runs) != 1 || thread.Runs[0].JobID != "next" {
		t.Fatalf("snapshot %+v %v", thread, err)
	}
	if len(run.Tools) != 1 || run.Tools[0].Status != "interrupted" {
		t.Fatal("recovery lost the interrupted call")
	}
	events, err := s.Events(t.Context(), "a", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var interrupted bool
	for _, event := range events {
		if event.Type == "tool.done" && event.Tool != nil && event.Tool.ID == "call" && event.Tool.Status == "interrupted" {
			interrupted = true
		}
	}
	if !interrupted {
		t.Fatal("interrupted tool.done was not replayable")
	}
	seq := run.Seq
	if err := s.RunEvent(t.Context(), "run.done", &run, nil); err != nil || run.Seq != seq {
		t.Fatalf("terminal replay duplicated %v", err)
	}
	run.Status = "running"
	if err := s.RunEvent(t.Context(), "run.update", &run, nil); err == nil {
		t.Fatal("terminal run resurrected")
	}
}

func TestCreationQueue(t *testing.T) {
	s := runStore(t)
	a := contract.Agent{ID: "new", Name: "New"}
	data := json.RawMessage(`{"agent":{"id":"new"},"prompt":"Help"}`)
	task := "Do this only after introducing yourself"
	if err := s.CreateDefinition(t.Context(), a, data, &task, nil); err != nil {
		t.Fatal(err)
	}
	job, err := s.Job(t.Context(), a.ID)
	if err != nil || job != "created:new" {
		t.Fatalf("intro order %s %v", job, err)
	}
	work, err := s.Work(t.Context(), job)
	if err != nil || work.Trigger != "created" || work.Message.ThreadID != a.ID {
		t.Fatalf("work %+v %v", work, err)
	}
	if err := s.Finish(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	job, err = s.Job(t.Context(), a.ID)
	if err != nil || job != "initial:new" {
		t.Fatalf("initial task order %s %v", job, err)
	}
	history, err := s.Messages(t.Context(), a.ID, 0, 20)
	if err != nil || len(history) != 1 || Text(history[0]) != task {
		t.Fatalf("synthetic intro leaked %+v %v", history, err)
	}
}

func TestPeerReceipts(t *testing.T) {
	s := runStore(t)
	parent := NewRun("request", "a", "a", "message")
	parent.Status = "running"
	if err := s.RunEvent(t.Context(), "run.start", &parent, nil); err != nil {
		t.Fatal(err)
	}
	text := "Only this explicit task"
	work := Work{Trigger: "peer", Message: contract.Message{ID: "delivery", ThreadID: "b", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}}, CreatedAt: Now()}, ParentRunID: &parent.ID, DeliveryID: "delivery"}
	delivery := contract.PeerDelivery{ID: "delivery", SourceAgentID: "a", TargetAgentID: "b", ConversationID: "a", ParentRunID: parent.ID, Intent: "request", Status: "queued", CreatedAt: Now()}
	if _, err := s.Deliver(t.Context(), delivery, work, true); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Events(t.Context(), "a", 0, 100)
	work.Message.CreatedAt = Now()
	if _, err := s.Deliver(t.Context(), delivery, work, true); err != nil {
		t.Fatalf("idempotent delivery rejected: %v", err)
	}
	work.Task = "Changed"
	if _, err := s.Deliver(t.Context(), delivery, work, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict %v", err)
	}
	after, _ := s.Events(t.Context(), "a", 0, 100)
	if len(after) != len(before) {
		t.Fatal("duplicate delivery emitted event")
	}
	job, err := s.Job(t.Context(), "b")
	if err != nil || job != "delivery" {
		t.Fatalf("peer not queued %s %v", job, err)
	}
	if _, err := s.Job(t.Context(), "b"); err == nil {
		t.Fatal("duplicate peer work")
	}
	run, err := s.Run(t.Context(), NewRun("delivery", "b", "b", "peer").ID)
	if err != nil {
		t.Fatal(err)
	}
	run.Status = "completed"
	if err := s.RunEvent(t.Context(), "run.done", &run, nil); err != nil {
		t.Fatal(err)
	}
	answer := "Result"
	result := contract.Message{Parts: []contract.Part{{Kind: "text", Text: &answer}}}
	for range 2 {
		if err := s.Receipt(t.Context(), "delivery", run, result); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := s.Messages(t.Context(), "a", 0, 100)
	if err != nil || len(messages) != 1 || Text(messages[0]) != answer || *messages[0].AgentID != "b" {
		t.Fatalf("receipt %+v %v", messages, err)
	}
}

func TestLeadPersists(t *testing.T) {
	s := runStore(t)
	group, err := s.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}, LeadAgentID: ptr("b")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateThread(t.Context(), group.ID, contract.UpdateThreadRequest{Title: new("Changed"), AgentIDs: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	group, err = s.Thread(t.Context(), group.ID)
	if err != nil || group.LeadAgentID == nil || *group.LeadAgentID != "b" {
		t.Fatalf("lead changed %+v %v", group, err)
	}
	if err := s.UpdateThread(t.Context(), group.ID, contract.UpdateThreadRequest{Title: new("Bad"), LeadAgentID: ptr("missing")}); !errors.Is(err, ErrMembers) {
		t.Fatal("invalid lead accepted", err)
	}
	if err := s.UpdateThread(t.Context(), group.ID, contract.UpdateThreadRequest{Title: new("Solo"), AgentIDs: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	group, _ = s.Thread(t.Context(), group.ID)
	if *group.LeadAgentID != "a" {
		t.Fatal("removed lead retained")
	}
}

func TestResumeInterrupted(t *testing.T) {
	for _, tc := range []struct {
		name    string
		attempt int64
		record  string
		live    bool
	}{
		{name: "error result", attempt: 1, record: `{"kind":"pi.tool-result","model":[{"isError":true}]}`},
		{name: "tool running", attempt: 1, record: `{"kind":"pi.tool-result","model":[{"isError":false}]}`, live: true},
		{name: "second attempt", attempt: 2, record: `{"kind":"pi.tool-result","model":[{"isError":false}]}`},
		{name: "missing durable", attempt: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := runStore(t)
			req := contract.SendRequest{ID: "turn", Text: "Hello", AttachmentIDs: []string{}}
			if _, _, err := s.Send(t.Context(), "a", req, []contract.Part{{Kind: "text", Text: &req.Text}}, "a"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Job(t.Context(), "a"); err != nil {
				t.Fatal(err)
			}
			run, err := s.Run(t.Context(), NewRun("turn", "a", "a", "message").ID)
			if err != nil {
				t.Fatal(err)
			}
			run.Status, run.Attempt = "running", tc.attempt
			if err := s.RunEvent(t.Context(), "run.update", &run, nil); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveSession(t.Context(), "a", "durable:1"); err != nil {
				t.Fatal(err)
			}
			if tc.record != "" {
				if err := writeDurable(t, s, tc.record, tc.live); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			run, err = s.Run(t.Context(), run.ID)
			if err != nil || run.Status != "interrupted" || run.Error == nil || run.Error.Message == "" {
				t.Fatalf("recovery %+v %v", run, err)
			}
		})
	}
}

func writeDurable(t *testing.T, s *Store, record string, live bool) error {
	t.Helper()
	var file string
	if err := s.DB.QueryRowContext(t.Context(), `SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&file); err != nil {
		return err
	}
	file = strings.TrimPrefix(file, "file:")
	if cut := strings.IndexByte(file, '?'); cut >= 0 {
		file = file[:cut]
	}
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(filepath.Dir(file), "durable.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE entries (id INTEGER PRIMARY KEY, conversation_id INTEGER NOT NULL, head INTEGER, commit_seq INTEGER NOT NULL, record TEXT NOT NULL);
CREATE TABLE tasks (id INTEGER PRIMARY KEY, conversation_id INTEGER NOT NULL, kind TEXT NOT NULL, status TEXT NOT NULL, abort_requested INTEGER NOT NULL, background INTEGER NOT NULL, record TEXT NOT NULL)`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO entries(id, conversation_id, head, commit_seq, record) VALUES (1, 1, NULL, 1, ?)`, record); err != nil {
		return err
	}
	if live {
		_, err = db.Exec(`INSERT INTO tasks(id, conversation_id, kind, status, abort_requested, background, record) VALUES (1, 1, 'pi.tool', 'running', 0, 0, '{}')`)
	}
	return err
}

func ptr(text string) *string { return &text }

func TestFinishIdentity(t *testing.T) {
	s := runStore(t)
	for _, test := range []struct{ id, thread string }{{"m", "a"}, {"m:next", "b"}, {"M", "b"}} {
		request := contract.SendRequest{ID: test.id, Text: "Hello", AttachmentIDs: []string{}}
		if _, _, err := s.Send(t.Context(), test.thread, request, []contract.Part{{Kind: "text", Text: &request.Text}}, test.thread); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Finish(t.Context(), "m"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"m:next", "M"} {
		run, err := s.Run(t.Context(), NewRun(id, "b", "b", "message").ID)
		if err != nil || run.Status != "queued" {
			t.Fatalf("unrelated job changed %+v %v", run, err)
		}
	}
}

func TestWithdrawAtomic(t *testing.T) {
	s := runStore(t)
	req := contract.SendRequest{ID: "queued", Text: "Hello", AttachmentIDs: []string{}}
	if _, _, err := s.Send(t.Context(), "a", req, []contract.Part{{Kind: "text", Text: &req.Text}}, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(t.Context(), `CREATE TRIGGER reject_done BEFORE INSERT ON events WHEN json_extract(NEW.data,'$.type')='run.done' BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Withdraw(t.Context(), "a"); err == nil {
		t.Fatal("fixture did not reject withdrawal")
	}
	var status string
	if err := s.DB.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id='queued'`).Scan(&status); err != nil || status != "queued" {
		t.Fatalf("withdrawal partially committed %s %v", status, err)
	}
	if _, err := s.DB.ExecContext(t.Context(), `DROP TRIGGER reject_done`); err != nil {
		t.Fatal(err)
	}
	if err := s.Withdraw(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	run, err := s.Run(t.Context(), NewRun("queued", "a", "a", "message").ID)
	if err != nil || run.Status != "cancelled" {
		t.Fatalf("withdrawn work %+v %v", run, err)
	}
}

func TestToolRunsSurviveNewerTurns(t *testing.T) {
	s := runStore(t)
	for _, id := range []string{"first", "second"} {
		req := contract.SendRequest{ID: id, Text: "Hello", AttachmentIDs: []string{}}
		if _, _, err := s.Send(t.Context(), "a", req, []contract.Part{{Kind: "text", Text: &req.Text}}, "a"); err != nil {
			t.Fatal(err)
		}
		run, err := s.Run(t.Context(), NewRun(id, "a", "a", "message").ID)
		if err != nil {
			t.Fatal(err)
		}
		run.Status = "completed"
		run.Tools = []contract.ToolActivity{{ID: id, RunID: run.ID, Name: "bash", Status: "failed", Summary: ptr("false"), Output: ptr("failed")}}
		if err := s.RunEvent(t.Context(), "run.done", &run, nil); err != nil {
			t.Fatal(err)
		}
		message := contract.Message{ID: id + "-reply", ThreadID: "a", AgentID: ptr("a"), Role: "assistant", RunID: &run.ID, Status: "done", CreatedAt: Now()}
		if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done", Message: &message}, &message); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := s.PageRuns(t.Context(), "a", 0, 100)
	if err != nil || len(runs) != 2 {
		t.Fatalf("relaunch rows: %d runs, %v", len(runs), err)
	}
	for _, run := range runs {
		if len(run.Tools) != 1 || run.Tools[0].Status != "failed" || *run.Tools[0].Output != "failed" {
			t.Fatal("failed tool row lost on replay")
		}
	}
	latest, err := s.PageRuns(t.Context(), "a", 0, 1)
	if err != nil || len(latest) != 1 || latest[0].ID != NewRun("second", "a", "a", "message").ID {
		t.Fatalf("latest page runs: %+v, %v", latest, err)
	}
	page, err := s.Messages(t.Context(), "a", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	earlier, err := s.PageRuns(t.Context(), "a", page[0].Seq, 2)
	if err != nil || len(earlier) != 1 || earlier[0].ID != NewRun("first", "a", "a", "message").ID {
		t.Fatalf("earlier page runs: %+v, %v", earlier, err)
	}
}

func TestCancelledToolHistory(t *testing.T) {
	s := runStore(t)
	run := NewRun("cancel", "a", "a", "message")
	run.Status = "running"
	run.Tools = []contract.ToolActivity{
		{ID: "done", RunID: run.ID, Name: "read", Status: "completed"},
		{ID: "failed", RunID: run.ID, Name: "bash", Status: "failed", Output: ptr("failure excerpt")},
		{ID: "active", RunID: run.ID, Name: "bash", Status: "running"},
	}
	if err := s.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StopRuns(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Run(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Tools) != 3 {
		t.Fatalf("cancel snapshot: %d rows, want 3", len(stored.Tools))
	}
	for i, status := range []string{"completed", "failed", "cancelled"} {
		if stored.Tools[i].Status != status {
			t.Errorf("row %d: %s, want %s", i, stored.Tools[i].Status, status)
		}
	}
	if stored.Tools[1].Output == nil || *stored.Tools[1].Output != "failure excerpt" {
		t.Fatal("failed excerpt lost")
	}
	events, err := s.Events(t.Context(), "a", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	for _, event := range events {
		if event.Type == "tool.done" {
			done++
			if event.Tool.ID != "active" || event.Tool.Status != "cancelled" {
				t.Errorf("finished row rewritten: %+v", event.Tool)
			}
		}
		if event.Type == "run.done" && len(event.Run.Tools) != 3 {
			t.Errorf("cancel replay: %d rows, want 3", len(event.Run.Tools))
		}
	}
	if done != 1 {
		t.Errorf("tool.done events: %d, want 1", done)
	}
}

func TestToolHistoryListBound(t *testing.T) {
	s := runStore(t)
	latest := NewRun("latest", "a", "a", "message")
	latest.Status = "completed"
	if err := s.RunEvent(t.Context(), "run.done", &latest, nil); err != nil {
		t.Fatal(err)
	}
	before, err := s.Threads(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	baseline, _ := json.Marshal(before)
	for i := 0; i < 50; i++ {
		run := NewRun(fmt.Sprint("old-", i), "a", "a", "message")
		run.Status, run.CreatedAt = "completed", "2020-01-01T00:00:00Z"
		run.Tools = []contract.ToolActivity{{ID: run.ID, RunID: run.ID, Name: "bash", Status: "completed", Output: ptr(strings.Repeat("x", 2000))}}
		if err := s.RunEvent(t.Context(), "run.done", &run, nil); err != nil {
			t.Fatal(err)
		}
	}
	threads, err := s.Threads(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(threads)
	if len(data) > len(baseline)+100 {
		t.Errorf("list payload grew: %d -> %d bytes", len(baseline), len(data))
	}
	for _, thread := range threads {
		if thread.ID == "a" && len(thread.Runs) != 1 {
			t.Errorf("list runs grew: 1 -> %d", len(thread.Runs))
		}
		for _, run := range thread.Runs {
			if len(run.Tools) != 0 {
				t.Error("list contains tool history")
			}
		}
	}
}

func TestRoutineToolHistory(t *testing.T) {
	s := runStore(t)
	run := NewRun("routine-history", "a", "a", "routine")
	run.Status = "running"
	run.Tools = []contract.ToolActivity{
		{ID: "done", RunID: run.ID, Status: "completed"},
		{ID: "failed", RunID: run.ID, Status: "failed", Output: ptr("failure excerpt")},
		{ID: "active", RunID: run.ID, Status: "running"},
		{ID: "active-2", RunID: run.ID, Status: "running"},
	}
	if err := s.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
		t.Fatal(err)
	}
	message := contract.Message{ID: routineNoticeID(run.ID), ThreadID: "a", Role: "system", Status: "done", CreatedAt: Now()}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.start"}, &message); err != nil {
		t.Fatal(err)
	}
	if sealed, err := s.sealRoutineFailure(t.Context(), &run); err != nil || !sealed {
		t.Fatalf("seal: %v, %v", sealed, err)
	}
	stored, err := s.Run(t.Context(), run.ID)
	if err != nil || len(stored.Tools) != 4 {
		t.Fatalf("routine rows: %+v, %v", stored, err)
	}
	for i, status := range []string{"completed", "failed", "failed", "failed"} {
		if stored.Tools[i].Status != status {
			t.Errorf("row %d: %s, want %s", i, stored.Tools[i].Status, status)
		}
	}
	if stored.Tools[1].Output == nil || *stored.Tools[1].Output != "failure excerpt" {
		t.Fatal("failed excerpt lost")
	}
	events, err := s.Events(t.Context(), "a", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	for _, event := range events {
		if event.Type == "tool.done" {
			done++
		}
	}
	if done != 2 {
		t.Fatalf("routine tool.done events: %d, want 2", done)
	}
}
