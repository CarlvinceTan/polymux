package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func futureTime() time.Time { return time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second) }

func routineFixture(t *testing.T, s *Store, id string, interval int64) contract.Routine {
	t.Helper()
	r, err := s.CreateRoutine(t.Context(), contract.Routine{
		ID: id, AgentID: "a", Prompt: "Remind me to review the draft", At: futureTime().Format(time.RFC3339),
		IntervalSeconds: interval, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestScheduleCoalesces(t *testing.T) {
	s := runStore(t)
	r := routineFixture(t, s, "daily", 86400)
	at, _ := time.Parse(time.RFC3339, r.At)
	now := at.Add(3*24*time.Hour + 30*time.Minute)
	for range 3 {
		if err := s.Schedule(t.Context(), now); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatal("missed fires produced a burst", count, err)
	}
	r, err := s.Routine(t.Context(), "a", r.ID)
	next, _ := time.Parse(time.RFC3339, valueString(r.NextAt))
	if err != nil || !next.Equal(at.Add(4*24*time.Hour)) {
		t.Fatal("recurrence drifted", next, err)
	}
	id, err := s.Job(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.Work(t.Context(), id)
	if err != nil || work.Trigger != "routine" || work.Message.Role != "system" || valueString(work.RoutineID) != r.ID {
		t.Fatalf("wrong scheduled work: %+v %v", work, err)
	}
	if err := s.Schedule(t.Context(), next.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Job(t.Context(), "a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("overlapping routine was queued", err)
	}
	if err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	run, err := s.Run(t.Context(), NewRun(id, "a", "a", "routine").ID)
	if err != nil || run.Status != "cancelled" {
		t.Fatal("claimed but unstarted run not cleaned up", run.Status, err)
	}
	if _, err := s.Job(t.Context(), "a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("ambiguous occurrence retried", err)
	}
}

func valueString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func TestRoutineCancellation(t *testing.T) {
	s := runStore(t)
	r := routineFixture(t, s, "once", 0)
	at, _ := time.Parse(time.RFC3339, r.At)
	if err := s.Schedule(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	ids, err := s.CancelRoutine(t.Context(), "b", r.ID)
	if !errors.Is(err, sql.ErrNoRows) || len(ids) != 0 {
		t.Fatal("cross-agent cancellation accepted", err)
	}
	ids, err = s.CancelRoutine(t.Context(), "a", r.ID)
	if err != nil || len(ids) != 1 {
		t.Fatal("missing cancelled run", ids, err)
	}
	if _, err := s.Job(t.Context(), "a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cancelled work still runnable", err)
	}
	run, err := s.Run(t.Context(), ids[0])
	if err != nil || run.Status != "cancelled" {
		t.Fatal("cancellation not durable", run.Status, err)
	}
	if err := s.Schedule(t.Context(), at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestRoutineApproval(t *testing.T) {
	s := runStore(t)
	r, err := s.CreateRoutine(t.Context(), contract.Routine{
		ID: "proposal", AgentID: "a", Prompt: "Prepare my draft", At: futureTime().Format(time.RFC3339), Status: "pending",
	})
	if err != nil {
		t.Fatal(err)
	}
	at, _ := time.Parse(time.RFC3339, r.At)
	if err := s.Schedule(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Job(t.Context(), "a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("proposal executed before approval", err)
	}
	send := func(thread, id, text string) error {
		request := contract.SendRequest{ID: id, Text: text, AttachmentIDs: []string{}}
		_, _, err := s.Send(t.Context(), thread, request, []contract.Part{{Kind: "text", Text: &text}}, thread)
		return err
	}
	if err := send("b", "wrong", "confirm routine proposal"); !errors.Is(err, ErrSchedule) {
		t.Fatal("cross-agent approval accepted", err)
	}
	if err := send("a", "quoted", "Someone said: confirm routine proposal"); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Routine(t.Context(), "a", r.ID)
	if r.Status != "pending" {
		t.Fatal("quoted approval activated routine")
	}
	for range 2 {
		if err := send("a", "approve", "confirm routine proposal"); err != nil {
			t.Fatal(err)
		}
	}
	r, _ = s.Routine(t.Context(), "a", r.ID)
	if r.Status != "active" {
		t.Fatal("exact user approval did not activate routine")
	}
	changed := r
	changed.Status, changed.Prompt = "pending", "Changed task"
	if _, err := s.CreateRoutine(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatal("approved proposal was mutable", err)
	}
}

func TestCreateRoutineRejectsInvalidSchedules(t *testing.T) {
	s := runStore(t)
	at := func(offset time.Duration) string {
		return time.Now().UTC().Add(offset).Truncate(time.Second).Format(time.RFC3339)
	}
	cases := []struct {
		name     string
		routine  contract.Routine
		rejected bool
	}{
		{"missing instant", contract.Routine{Prompt: "Check the draft"}, true},
		{"malformed instant", contract.Routine{At: "tomorrow morning", Prompt: "Check the draft"}, true},
		{"instant in the past", contract.Routine{At: at(-2 * time.Hour), Prompt: "Check the draft"}, true},
		{"instant inside grace", contract.Routine{At: at(-30 * time.Second), Prompt: "Check the draft"}, false},
		{"sub-minute interval", contract.Routine{At: at(time.Hour), Prompt: "Check the draft", IntervalSeconds: 30}, true},
		{"negative interval", contract.Routine{At: at(time.Hour), Prompt: "Check the draft", IntervalSeconds: -60}, true},
		{"interval above ceiling", contract.Routine{At: at(time.Hour), Prompt: "Check the draft", IntervalSeconds: 31536001}, true},
		{"minute interval", contract.Routine{At: at(time.Hour), Prompt: "Check the draft", IntervalSeconds: 60}, false},
		{"ceiling interval", contract.Routine{At: at(time.Hour), Prompt: "Check the draft", IntervalSeconds: 31536000}, false},
		{"blank prompt", contract.Routine{At: at(time.Hour), Prompt: "   "}, true},
		{"oversized prompt", contract.Routine{At: at(time.Hour), Prompt: strings.Repeat("a", 4097)}, true},
		{"bounded prompt", contract.Routine{At: at(time.Hour), Prompt: strings.Repeat("a", 4096)}, false},
		{"unknown status", contract.Routine{At: at(time.Hour), Prompt: "Check the draft", Status: "draft"}, true},
	}
	// A distinct agent per case keeps the per-agent routine limit and the shared ID space out of the result.
	agents := make([]contract.Agent, len(cases))
	for i := range cases {
		agents[i] = contract.Agent{ID: fmt.Sprintf("agent-%d", i), Name: "Schedule"}
	}
	if err := s.Register(t.Context(), agents); err != nil {
		t.Fatal(err)
	}
	for i, test := range cases {
		r := test.routine
		r.ID, r.AgentID = fmt.Sprintf("routine-%d", i), fmt.Sprintf("agent-%d", i)
		if r.Status == "" {
			r.Status = "pending"
		}
		_, err := s.CreateRoutine(t.Context(), r)
		if test.rejected {
			if !errors.Is(err, ErrSchedule) {
				t.Fatalf("%s accepted: %v", test.name, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s rejected: %v", test.name, err)
		}
		if stored, err := s.Routine(t.Context(), r.AgentID, r.ID); err != nil || stored.At != r.At {
			t.Fatalf("%s not stored: %+v %v", test.name, stored, err)
		}
	}
}

func TestReviewsRequireTask(t *testing.T) {
	s := runStore(t)
	p, err := s.Proactivity(t.Context(), "a")
	if err != nil || p.Enabled || p.NextAt != nil {
		t.Fatal("creation alone enabled initiative", p, err)
	}
	request := contract.SendRequest{ID: "task", Text: "Help with my draft", AttachmentIDs: []string{}}
	if _, _, err := s.Send(t.Context(), "a", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "a"); err != nil {
		t.Fatal(err)
	}
	p, err = s.Proactivity(t.Context(), "a")
	if err != nil || !p.Enabled || p.NextAt == nil || p.IntervalSeconds != 14400 {
		t.Fatal("task did not enable bounded reviews", p, err)
	}
	at, _ := time.Parse(time.RFC3339, *p.NextAt)
	if err := s.Schedule(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	var reviews int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM runs
WHERE json_extract(data,'$.trigger')='review'`).Scan(&reviews); err != nil || reviews != 0 {
		t.Fatal("review overtook user work", reviews, err)
	}
	if err := s.Finish(t.Context(), request.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Schedule(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	id, err := s.Job(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.Work(t.Context(), id)
	if err != nil || work.Trigger != "review" {
		t.Fatal("review not runnable", work.Trigger, err)
	}
	request.ID, request.Text = "pause", "pause proactivity"
	if _, _, err := s.Send(t.Context(), "a", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "a"); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Proactivity(t.Context(), "a")
	if p.Enabled || p.NextAt != nil {
		t.Fatal("pause not durable", p)
	}
}

func TestReviewPublication(t *testing.T) {
	s := runStore(t)
	if err := s.SetProactivity(t.Context(), "a", contract.Proactivity{Enabled: true, IntervalSeconds: 14400}); err != nil {
		t.Fatal(err)
	}
	text := "Your draft is still waiting for the outline you requested."
	for i, body := range []string{"", text, text} {
		run := NewRun(fmt.Sprintf("review-%d", i), "a", "a", "review")
		run.Status = "running"
		if err := s.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
			t.Fatal(err)
		}
		run.Status = "completed"
		m := contract.Message{ID: "reply:" + run.ID, ThreadID: "a", Role: "assistant", Status: "done",
			AgentID: ptr("a"), CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &body}}}
		if err := s.FinalizeReview(t.Context(), &m, &run); err != nil {
			t.Fatal(err)
		}
		want := "quiet"
		if i == 1 {
			want = "notice"
		}
		if run.Outcome == nil || *run.Outcome != want {
			t.Fatal("unexpected review outcome", run.Outcome)
		}
		if err := s.FinalizeReview(t.Context(), &m, &run); err != nil {
			t.Fatal("replayed review", err)
		}
	}
	messages, err := s.Messages(t.Context(), "a", 0, 100)
	if err != nil || len(messages) != 1 || Text(messages[0]) != text {
		t.Fatal("quiet/replayed review polluted chat", len(messages), err)
	}
}

func TestReviewNoticeWindowSuppressesOlderFindings(t *testing.T) {
	s := runStore(t)
	if err := s.SetProactivity(t.Context(), "a", contract.Proactivity{Enabled: true, IntervalSeconds: 14400}); err != nil {
		t.Fatal(err)
	}
	review := func(i int, body string) string {
		t.Helper()
		run := NewRun(fmt.Sprintf("window-%d", i), "a", "a", "review")
		run.Status = "running"
		if err := s.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
			t.Fatal(err)
		}
		run.Status = "completed"
		m := contract.Message{ID: "reply:" + run.ID, ThreadID: "a", Role: "assistant", Status: "done",
			AgentID: ptr("a"), CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &body}}}
		if err := s.FinalizeReview(t.Context(), &m, &run); err != nil {
			t.Fatal(err)
		}
		if run.Outcome == nil {
			return ""
		}
		return *run.Outcome
	}
	first, second := "Outline is still missing.", "The invoice is unpaid."
	for i, want := range []string{"notice", "notice", "quiet", "quiet"} {
		if got := review(i, []string{first, second, first, first}[i]); got != want {
			t.Fatalf("step %d reported %q, want %q", i, got, want)
		}
	}
	// The window is bounded, so a long-silent finding becomes reportable again.
	for i := range NoticeWindow {
		review(100+i, fmt.Sprintf("Distinct finding %d.", i))
	}
	if got := review(200, first); got != "notice" {
		t.Fatalf("evicted finding stayed suppressed: %q", got)
	}
}

func TestCancelReviewsWithdrawsQueuedWork(t *testing.T) {
	s := runStore(t)
	text := "Help with my draft"
	if _, _, err := s.Send(t.Context(), "a", contract.SendRequest{ID: "task", Text: text, AttachmentIDs: []string{}}, []contract.Part{{Kind: "text", Text: &text}}, "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	at, _ := time.Parse(time.RFC3339, valueString(mustProactivity(t, s, "a").NextAt))
	if err := s.Schedule(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	job, err := s.Job(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.Work(t.Context(), job)
	if err != nil || work.Trigger != "review" {
		t.Fatalf("review not queued: %+v %v", work, err)
	}
	ids, err := s.CancelReviews(t.Context(), "a")
	if err != nil || len(ids) != 1 {
		t.Fatalf("queued review not withdrawn: %v %v", ids, err)
	}
	if _, err := s.Job(t.Context(), "a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("withdrawn review stayed runnable", err)
	}
	run, err := s.Run(t.Context(), ids[0])
	if err != nil || run.Status != "cancelled" {
		t.Fatalf("review not cancelled: %+v %v", run, err)
	}
	// A disabled cadence reports nothing further to cancel.
	if err := s.SetProactivity(t.Context(), "a", contract.Proactivity{Enabled: false, IntervalSeconds: 14400}); err != nil {
		t.Fatal(err)
	}
	if ids, err := s.CancelReviews(t.Context(), "a"); err != nil || len(ids) != 0 {
		t.Fatalf("cancelled review reappeared: %v %v", ids, err)
	}
}

func mustProactivity(t *testing.T, s *Store, agent string) contract.Proactivity {
	t.Helper()
	p, err := s.Proactivity(t.Context(), agent)
	if err != nil || p.NextAt == nil {
		t.Fatalf("proactivity %+v %v", p, err)
	}
	return p
}

func TestZonedRoutineKeepsLocalTimeAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("zone data unavailable")
	}
	// 09:00 the day before US daylight saving starts, so the next local day is 23 hours long.
	start := time.Date(2026, 3, 7, 9, 0, 0, 0, loc)
	zone := "America/New_York"
	now := start.Add(time.Hour).Unix()
	due := start.Unix()
	got := time.Unix(nextRoutineFire(contract.Routine{Timezone: &zone, IntervalSeconds: 86400}, due, now), 0).In(loc)
	if got.Hour() != 9 || got.Minute() != 0 || got.Day() != 8 {
		t.Fatalf("wall clock drifted: %s", got)
	}
	elapsed := time.Unix(due+((now-due)/86400+1)*86400, 0).In(loc)
	if elapsed.Hour() == 9 {
		t.Fatalf("fixture does not cross a transition: %s", elapsed)
	}
	// Without a zone the elapsed cadence is unchanged.
	plain := time.Unix(nextRoutineFire(contract.Routine{IntervalSeconds: 86400}, due, now), 0).UTC()
	if plain.Unix() != elapsed.Unix() {
		t.Fatalf("unzoned routine changed cadence: %s", plain)
	}
}

func TestCreateRoutineRejectsBadTimezones(t *testing.T) {
	s := runStore(t)
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339)
	for _, zone := range []string{"Mars/Olympus", "America/New_York ", "Australia/Sydney"} {
		_, err := s.CreateRoutine(t.Context(), contract.Routine{
			ID: "zone-" + zone, AgentID: "a", Prompt: "Check the draft", At: at,
			IntervalSeconds: 86400, Status: "active", Timezone: &zone,
		})
		want := errors.Is(err, ErrSchedule)
		if valid := zone == "Australia/Sydney" || zone == "America/New_York "; valid {
			want = false
		}
		if want != (err != nil) {
			t.Fatalf("zone %q: %v", zone, err)
		}
	}
}

func TestOpenWorkScope(t *testing.T) {
	s := runStore(t)
	group, err := s.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"a", group.ID} {
		run := NewRun("work", scope, "a", "message")
		if err := s.SaveWork(t.Context(), run, WorkItem{ID: "item", Text: "Private " + scope, State: "open"}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.OpenWork(t.Context(), "b", group.ID)
	if err != nil || len(items) != 0 {
		t.Fatal("member read another agent's scratchpad", items, err)
	}
	items, err = s.OpenWork(t.Context(), "a", group.ID)
	if err != nil || len(items) != 1 || strings.Contains(items[0].Text, "Private a") {
		t.Fatal("private scratchpad crossed group scope", items, err)
	}
	routineFixture(t, s, "cleanup", 86400)
	if _, _, err := s.DeleteAgent(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"routines", "open_work", "proactivity"} {
		var count int
		if err := s.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("proactive state survived deletion", table, count, err)
		}
	}
}

func TestRoutineOffsets(t *testing.T) {
	s := runStore(t)
	at := futureTime().In(time.FixedZone("offset", -7*3600))
	r, err := s.CreateRoutine(t.Context(), contract.Routine{
		ID: "offset", AgentID: "a", Prompt: "Daily reminder", At: at.Format(time.RFC3339),
		IntervalSeconds: 86400, Status: "active",
	})
	if err != nil || !strings.HasSuffix(r.At, "Z") {
		t.Fatal("offset not normalized", r.At, err)
	}
	if err := s.Schedule(t.Context(), at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Routine(t.Context(), "a", r.ID)
	next, _ := time.Parse(time.RFC3339, valueString(r.NextAt))
	if next.Sub(at) != 24*time.Hour {
		t.Fatal("fixed elapsed-time schedule changed with timezone", next)
	}
	encoded, _ := json.Marshal(r)
	if !json.Valid(encoded) {
		t.Fatal("invalid routine wire data")
	}
}

func TestBusyAgentCannotStarveSchedule(t *testing.T) {
	s := runStore(t)
	for i := range 32 {
		routineFixture(t, s, fmt.Sprintf("busy-%02d", i), 86400)
	}
	run := NewRun("waiting", "a", "a", "message")
	run.Status = "waiting_peer"
	if err := s.RunEvent(t.Context(), "run.update", &run, nil); err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRoutine(t.Context(), contract.Routine{
		ID: "free", AgentID: "b", Prompt: "My independent reminder", At: futureTime().Format(time.RFC3339), Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	at, _ := time.Parse(time.RFC3339, r.At)
	if err := s.Schedule(t.Context(), at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Job(t.Context(), "b"); err != nil {
		t.Fatal("busy-agent batch starved unrelated routine", err)
	}
}

func TestActiveRoutinesRemainDiscoverable(t *testing.T) {
	s := runStore(t)
	routineFixture(t, s, "active", 86400)
	for i := range 65 {
		r := routineFixture(t, s, fmt.Sprintf("old-%d", i), 0)
		if _, err := s.CancelRoutine(t.Context(), "a", r.ID); err != nil {
			t.Fatal(err)
		}
	}
	routines, err := s.Routines(t.Context(), "a")
	if err != nil || len(routines) != 64 || routines[0].ID != "active" {
		t.Fatal("routine history hid active work", len(routines), err)
	}
}

func TestR4NoDuplicateOnRecover(t *testing.T) {
	s := runStore(t)
	if err := s.RegisterPush(t.Context(), contract.PushDevice{ID: "fixture-installation", Token: strings.Repeat("ab", 32), Environment: "sandbox", Server: "http://server.test:7337", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	routine, err := s.CreateRoutine(t.Context(), contract.Routine{ID: "morning", AgentID: "a", Prompt: "Morning brief", Status: "active", At: futureTime().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	run := NewRun("morning", "a", "a", "routine")
	run.RoutineID, run.Status = &routine.ID, "running"
	if err := s.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
		t.Fatal(err)
	}
	run.Status, run.Error = "failed", &contract.Error{Code: "provider", Message: "500 internal server error"}
	for range 2 {
		if err := s.routineNotice(t.Context(), &run); err != nil {
			t.Fatal(err)
		}
	}
	if open, err := s.Run(t.Context(), run.ID); err != nil || open.Status != "running" {
		t.Fatal("notice commit sealed the run", open.Status, err)
	}
	for range 2 {
		if err := s.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.routineNotice(t.Context(), &run); err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Run(t.Context(), run.ID)
	messages, msgErr := s.Messages(t.Context(), "a", 0, 100)
	notices := 0
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Kind == "routine_failed" {
				notices++
			}
		}
	}
	var pushes int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM notices`).Scan(&pushes); err != nil {
		t.Fatal(err)
	}
	if err != nil || msgErr != nil || sealed.Status != "failed" || sealed.Error == nil || sealed.Error.Code != "provider" || notices != 1 || pushes != 1 {
		t.Fatalf("recovered %+v notices %d pushes %d %v", sealed, notices, pushes, err)
	}
}
