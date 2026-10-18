package evals

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func activityStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "activity.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, id := range []string{"chief", "aria"} {
		a := contract.Agent{ID: id, Name: id}
		if err := db.Register(t.Context(), []contract.Agent{a}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestU15(t *testing.T) {
	db := activityStore(t)
	routine, err := db.CreateRoutine(t.Context(), contract.Routine{ID: "routine", AgentID: "chief", Prompt: "Check sessions", At: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := db.Thread(t.Context(), "chief")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CancelRoutine(t.Context(), "chief", routine.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CancelRoutine(t.Context(), "chief", routine.ID); err != nil {
		t.Fatal(err)
	}
	text := "Review the public task"
	delivery := contract.PeerDelivery{ID: "exchange", ConversationID: "chief", SourceAgentID: "chief", TargetAgentID: "aria", Intent: "message", Status: "delivered"}
	work := store.Work{Trigger: "peer", Message: contract.Message{ID: "request", ThreadID: "aria", CreatedAt: store.Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}}
	if _, err := db.Deliver(t.Context(), delivery, work, false); err != nil {
		t.Fatal(err)
	}
	events, err := db.Events(t.Context(), "chief", before.LastSeq, 100)
	if err != nil {
		t.Fatal(err)
	}
	activities := []contract.Event{}
	for _, event := range events {
		if event.Type == "activity" {
			activities = append(activities, event)
		}
	}
	if len(activities) != 2 {
		t.Fatalf("want two rows, got %d", len(activities))
	}
	for i, kind := range []string{"routine.deleted", "agent.messaged"} {
		if activities[i].Payload.Kind != kind || activities[i].Message != nil {
			t.Fatalf("wrong row: %+v", activities[i])
		}
	}
	mirrored, err := db.Events(t.Context(), "aria", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(mirrored) != 2 || mirrored[1].Payload.Verb != "Messaged" {
		t.Fatal("missing target activity")
	}
	history, err := db.PeerHistory(t.Context(), "chief", "aria", 0)
	if err != nil || len(history) != 1 || store.Text(history[0]) != text {
		t.Fatalf("incorrect peer history: %+v %v", history, err)
	}
	// Delivery replay must not duplicate either history or activity.
	if _, err := db.Deliver(t.Context(), delivery, work, false); err != nil {
		t.Fatal(err)
	}
	replay, _ := db.Events(t.Context(), "chief", before.LastSeq, 100)
	if len(replay) != len(events) {
		t.Fatal("duplicate delivery emitted another activity")
	}
	peerReply(t, db)
	pass(t, "U15", "routine and peer activities persist without bubbles or duplicate delivery rows")
}

func peerReply(t *testing.T, db *store.Store) {
	t.Helper()
	parent := store.NewRun("reply-parent", "chief", "chief", "message")
	if err := db.RunEvent(t.Context(), "run.start", &parent, nil); err != nil {
		t.Fatal(err)
	}
	text := "Check the task"
	work := store.Work{Trigger: "peer", ParentRunID: &parent.ID,
		Message: contract.Message{ID: "reply-request", ThreadID: "aria", CreatedAt: store.Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}}
	delivery := contract.PeerDelivery{ID: "reply-exchange", ParentRunID: parent.ID, ConversationID: "chief", SourceAgentID: "chief", TargetAgentID: "aria", Intent: "request", Status: "queued"}
	saved, err := db.Deliver(t.Context(), delivery, work, true)
	if err != nil {
		t.Fatal(err)
	}
	child := store.NewRun(work.Message.ID, "aria", "aria", "peer")
	child.ParentRunID, child.Status = &parent.ID, "completed"
	if err := db.RunEvent(t.Context(), "run.done", &child, nil); err != nil {
		t.Fatal(err)
	}
	text = "Task checked"
	result := contract.Message{ID: "reply:" + child.ID, Parts: []contract.Part{{Kind: "text", Text: &text}}}
	for range 2 {
		if err := db.Receipt(t.Context(), saved.ID, child, result); err != nil {
			t.Fatal(err)
		}
	}
	for _, thread := range []string{"chief", "aria"} {
		events, err := db.Events(t.Context(), thread, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		activities := []contract.Event{}
		for _, event := range events {
			if event.Payload != nil && event.Payload.Kind == "agent.messaged" && event.Payload.ObjectID == saved.ID {
				activities = append(activities, event)
			}
		}
		if len(activities) != 2 || !slices.Contains(activities[0].Payload.MessageIDs, result.ID) || !slices.Contains(activities[1].Payload.MessageIDs, "receipt:"+saved.ID) {
			t.Fatalf("request and reply must emit exactly once and hide peer bubbles in %s: %+v", thread, activities)
		}
	}
	history, err := db.PeerHistory(t.Context(), "chief", "aria", 0)
	if err != nil || len(history) != 3 || store.Text(history[0]) != text {
		t.Fatalf("reply missing from history: %+v %v", history, err)
	}
}

func TestU16(t *testing.T) {
	db := activityStore(t)
	cases := []struct{ kind, verb, icon string }{
		{"agent.messaged", "Messaged", ""}, {"routine.created", "Created", "clock"},
		{"routine.updated", "Updated", "clock"}, {"routine.deleted", "Deleted", "clock"},
		{"routine.paused", "Paused", "clock"}, {"routine.resumed", "Resumed", "clock"},
		{"routine.fired", "Ran", "clock"}, {"memory.saved", "Saved", "brain"},
		{"skill.saved", "Saved", "sparkles"}, {"subagent.started", "Started", "arrow.triangle.branch"},
		{"subagent.finished", "Finished", "arrow.triangle.branch"},
		{"connector.added", "Connected", "powerplug"}, {"connector.removed", "Disconnected", "powerplug"},
	}
	for _, test := range cases {
		payload, err := store.NewActivity(test.kind, "object", "Object label")
		if err != nil {
			t.Fatal(test.kind, err)
		}
		if err := db.Activity(t.Context(), "chief", "chief", payload); err != nil {
			t.Fatal(test.kind, err)
		}
		events, err := db.Events(t.Context(), "chief", 0, 100)
		if err != nil {
			t.Fatal(test.kind, err)
		}
		row := events[len(events)-1]
		if row.Type != "activity" || row.Payload.Verb != test.verb || row.Payload.IconOrAgentID != test.icon || row.Payload.ObjectLabel != "Object label" || row.Message != nil {
			t.Fatalf("%s: incorrect activity: %+v", test.kind, row)
		}
		encoded, err := json.Marshal(row)
		if err != nil || !strings.Contains(string(encoded), `"payload"`) {
			t.Fatal(test.kind, "missing wire payload", err)
		}
	}

	r, err := db.CreateRoutine(t.Context(), contract.Routine{ID: "mutable", AgentID: "chief", Prompt: "Daily check", At: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	prompt, enabled := "Updated check", false
	if _, err := db.PatchRoutine(t.Context(), "chief", r.ID, store.RoutinePatch{Prompt: &prompt, Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	enabled = true
	if _, err := db.PatchRoutine(t.Context(), "chief", r.ID, store.RoutinePatch{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	events, _ := db.Events(t.Context(), "chief", 0, 100)
	tail := events[len(events)-4:]
	for i, kind := range []string{"routine.created", "routine.updated", "routine.paused", "routine.resumed"} {
		if tail[i].Payload.Kind != kind {
			t.Fatal("routine operation did not emit", kind)
		}
	}
	parent := store.NewRun("parent", "chief", "chief", "message")
	if err := db.RunEvent(t.Context(), "run.start", &parent, nil); err != nil {
		t.Fatal(err)
	}
	child := store.NewRun("child", "aria", "aria", "peer")
	child.ParentRunID = &parent.ID
	for range 2 {
		if err := db.RunEvent(t.Context(), "run.start", &child, nil); err != nil {
			t.Fatal(err)
		}
	}
	child.Status = "completed"
	if err := db.RunEvent(t.Context(), "run.done", &child, nil); err != nil {
		t.Fatal(err)
	}
	for _, thread := range []string{"chief", "aria"} {
		events, err := db.Events(t.Context(), thread, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		kinds := []string{}
		for _, event := range events {
			if event.Payload != nil && event.Payload.ObjectID == child.ID && strings.HasPrefix(event.Payload.Kind, "subagent.") {
				kinds = append(kinds, event.Payload.Kind)
			}
		}
		if strings.Join(kinds, ",") != "subagent.started,subagent.finished" {
			t.Fatalf("child lifecycle in %s: %v", thread, kinds)
		}
	}
	pass(t, "U16", "all activity kinds carry labels and icons; routine and child transitions emit once")
}

func TestU17(t *testing.T) {
	db := activityStore(t)
	base := time.Now().UTC()
	for i := range 3 {
		payload, _ := store.NewActivity("memory.saved", "preference", "Preference")
		payload.At = base.Add(time.Duration(i) * 20 * time.Second).Format(time.RFC3339)
		if err := db.Activity(t.Context(), "chief", "chief", payload); err != nil {
			t.Fatal(err)
		}
	}
	events, err := db.Events(t.Context(), "chief", 0, 100)
	if err != nil || len(events) != 3 {
		t.Fatalf("want three durable events: %d %v", len(events), err)
	}
	// The client collapses presentation; the durable stream must retain all evidence.
	for i, event := range events {
		if event.Seq != int64(i+1) || event.Payload.Kind != "memory.saved" || event.Message != nil {
			t.Fatal("lost activity evidence")
		}
	}
	threads, err := db.Threads(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, thread := range threads {
		if thread.UnreadCount != 0 {
			t.Fatal("memory activity counted as unread")
		}
	}
	pass(t, "U17", "three memory activities remain durable for client collapse without unread counts")
}
