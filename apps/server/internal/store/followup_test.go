package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestFollowupReceipts(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(fmt.Sprint("early-", early), func(t *testing.T) {
			s := runStore(t)
			request := contract.SendRequest{ID: "source", Text: "Compare the two summaries", AttachmentIDs: []string{}}
			if _, _, err := s.Send(t.Context(), "a", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "a"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Job(t.Context(), "a"); err != nil {
				t.Fatal(err)
			}
			parent, _ := s.Run(t.Context(), NewRun("source", "a", "a", "message").ID)
			parent.Status = "running"
			if err := s.RunEvent(t.Context(), "run.update", &parent, nil); err != nil {
				t.Fatal(err)
			}
			for i := range 2 {
				id := fmt.Sprintf("peer-%d", i)
				text := "Only this bounded request"
				work := Work{Trigger: "peer", Message: contract.Message{
					ID: id, ThreadID: "b", Role: "user", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}},
				}, ParentRunID: &parent.ID, DeliveryID: id}
				delivery := contract.PeerDelivery{ID: id, SourceAgentID: "a", TargetAgentID: "b",
					ConversationID: "a", ParentRunID: parent.ID, Intent: "request", Status: "queued", CreatedAt: Now()}
				if _, err := s.Deliver(t.Context(), delivery, work, true); err != nil {
					t.Fatal(err)
				}
			}
			finalizeParent := func() {
				t.Helper()
				parent.Status = "completed"
				if err := s.RunEvent(t.Context(), "run.done", &parent, nil); err != nil {
					t.Fatal(err)
				}
			}
			if !early {
				finalizeParent()
			}
			for i := range 2 {
				id := fmt.Sprintf("peer-%d", i)
				child, _ := s.Run(t.Context(), NewRun(id, "b", "b", "peer").ID)
				child.Status = "completed"
				if i == 1 {
					child.Status = "failed"
				}
				if err := s.RunEvent(t.Context(), "run.done", &child, nil); err != nil {
					t.Fatal(err)
				}
				answer := "Peer supplied a usable summary"
				for range 2 {
					if err := s.Receipt(t.Context(), id, child, contract.Message{Parts: []contract.Part{{Kind: "text", Text: &answer}}}); err != nil {
						t.Fatal(err)
					}
				}
				if i == 0 || early {
					if _, err := s.Work(t.Context(), "followup:"+parent.ID); !errors.Is(err, sql.ErrNoRows) {
						t.Fatal("continuation raced incomplete source/peer work", err)
					}
				}
			}
			if early {
				finalizeParent()
			}
			work, err := s.Work(t.Context(), "followup:"+parent.ID)
			text := Text(work.Message)
			valid := work.Trigger == "follow_up" && valueString(work.ParentRunID) == parent.ID
			if err != nil || !valid || !strings.Contains(text, request.Text) || !strings.Contains(text, "usable summary") || !strings.Contains(text, "failed") {
				t.Fatal("missing correlated continuation context", valid, err)
			}
			var count int
			if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs WHERE id=?`, work.Message.ID).Scan(&count); err != nil || count != 1 {
				t.Fatal("receipt replay duplicated continuation", count, err)
			}
			if err := s.Finish(t.Context(), "source"); err != nil {
				t.Fatal(err)
			}
			if err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if id, err := s.Job(t.Context(), "a"); err != nil || id != work.Message.ID {
				t.Fatal("queued continuation lost during recovery", id, err)
			}
		})
	}
}

func TestStoppedPeersDoNotContinue(t *testing.T) {
	s := runStore(t)
	request := contract.SendRequest{ID: "source", Text: "Delegate this", AttachmentIDs: []string{}}
	if _, _, err := s.Send(t.Context(), "a", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "a"); err != nil {
		t.Fatal(err)
	}
	parent, _ := s.Run(t.Context(), NewRun("source", "a", "a", "message").ID)
	parent.Status = "running"
	if err := s.RunEvent(t.Context(), "run.update", &parent, nil); err != nil {
		t.Fatal(err)
	}
	text := "A task"
	work := Work{Trigger: "peer", Message: contract.Message{ID: "delivery", ThreadID: "b", Role: "user",
		CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}, ParentRunID: &parent.ID, DeliveryID: "delivery"}
	d := contract.PeerDelivery{ID: "delivery", SourceAgentID: "a", TargetAgentID: "b", ConversationID: "a",
		ParentRunID: parent.ID, Intent: "request", Status: "queued", CreatedAt: Now()}
	if _, err := s.Deliver(t.Context(), d, work, true); err != nil {
		t.Fatal(err)
	}
	parent.Status = "completed"
	if err := s.RunEvent(t.Context(), "run.done", &parent, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StopWaiting(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Work(t.Context(), "followup:"+parent.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("stop generated an unsolicited continuation", err)
	}
}

func TestPeerWaitSurvivesRestart(t *testing.T) {
	s := runStore(t)
	request := contract.SendRequest{ID: "source", Text: "Summarize delegated results", AttachmentIDs: []string{}}
	if _, _, err := s.Send(t.Context(), "a", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Job(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	parent, _ := s.Run(t.Context(), NewRun("source", "a", "a", "message").ID)
	parent.Status = "running"
	if err := s.RunEvent(t.Context(), "run.update", &parent, nil); err != nil {
		t.Fatal(err)
	}
	text := "Public delegated task"
	work := Work{Trigger: "peer", DeliveryID: "peer", ParentRunID: &parent.ID, Message: contract.Message{
		ID: "peer", ThreadID: "b", Role: "user", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}},
	}}
	d := contract.PeerDelivery{ID: "peer", SourceAgentID: "a", TargetAgentID: "b", ConversationID: "a",
		ParentRunID: parent.ID, Intent: "request", Status: "queued", CreatedAt: Now()}
	if _, err := s.Deliver(t.Context(), d, work, true); err != nil {
		t.Fatal(err)
	}
	message := contract.Message{ID: "reply:" + parent.ID, ThreadID: "a", Role: "assistant", Status: "done",
		CreatedAt: Now(), AgentID: ptr("a"), RunID: &parent.ID, Parts: []contract.Part{{Kind: "text", Text: &text}}}
	parent.Status, parent.MessageID = "completed", &message.ID
	if err := s.Finalize(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &message, &parent, nil); err != nil {
		t.Fatal(err)
	}
	if parent.Status != "waiting_peer" {
		t.Fatal("source did not wait", parent.Status)
	}
	if err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	parent, _ = s.Run(t.Context(), parent.ID)
	if parent.Status != "waiting_peer" {
		t.Fatal("restart destroyed durable source wait", parent.Status)
	}
	child, _ := s.Run(t.Context(), NewRun("peer", "b", "b", "peer").ID)
	child.Status = "completed"
	if err := s.RunEvent(t.Context(), "run.done", &child, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Receipt(t.Context(), "peer", child, message); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Work(t.Context(), "followup:"+parent.ID); err != nil {
		t.Fatal("durable peer completion did not wake source after restart", err)
	}
}

func TestTimeoutFollowupContext(t *testing.T) {
	s := runStore(t)
	request := contract.SendRequest{ID: "source", Text: "Watch the background task", AttachmentIDs: []string{}}
	if _, _, err := s.Send(t.Context(), "a", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Job(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	parent, _ := s.Run(t.Context(), NewRun("source", "a", "a", "message").ID)
	parent.Status = "running"
	if err := s.RunEvent(t.Context(), "run.update", &parent, nil); err != nil {
		t.Fatal(err)
	}
	id := "peer-timeout"
	text := "Hold this"
	work := Work{Trigger: "peer", DeliveryID: id, ParentRunID: &parent.ID, Message: contract.Message{
		ID: id, ThreadID: "b", Role: "user", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}},
	}}
	delivery := contract.PeerDelivery{ID: id, SourceAgentID: "a", TargetAgentID: "b", ConversationID: "a",
		ParentRunID: parent.ID, Intent: "request", Status: "queued", CreatedAt: Now()}
	if _, err := s.Deliver(t.Context(), delivery, work, true); err != nil {
		t.Fatal(err)
	}
	parent.Status = "completed"
	if err := s.RunEvent(t.Context(), "run.done", &parent, nil); err != nil {
		t.Fatal(err)
	}
	child, _ := s.Run(t.Context(), NewRun(id, "b", "b", "peer").ID)
	note := "The background task timed out and was stopped."
	child.Status = "failed"
	child.Error = &contract.Error{Code: "timeout", Message: note}
	if err := s.RunEvent(t.Context(), "run.done", &child, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Receipt(t.Context(), id, child, contract.Message{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Receipt(t.Context(), id, child, contract.Message{}); err != nil {
		t.Fatal(err)
	}
	var receipts int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM messages WHERE id=?`, "receipt:"+id).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatal("timeout posted a receipt", receipts, err)
	}
	follow, err := s.Work(t.Context(), "followup:"+parent.ID)
	body := Text(follow.Message)
	if err != nil || follow.Trigger != "follow_up" || !strings.Contains(body, note) || !strings.Contains(body, `"status":"failed"`) {
		t.Fatal("missing timeout continuation context", follow.Trigger, body, err)
	}
	for _, message := range mustMessages(t, s, "a") {
		if strings.HasPrefix(message.ID, "receipt:") || strings.Contains(Text(message), note) {
			t.Fatal("timeout note landed in the 1:1 thread", message)
		}
	}
}

func TestGroupTimeoutChat(t *testing.T) {
	s := runStore(t)
	group, err := s.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	request := contract.SendRequest{ID: "source", Text: "Ask B in the group", AttachmentIDs: []string{}}
	if _, _, err := s.Send(t.Context(), group.ID, request, []contract.Part{{Kind: "text", Text: &request.Text}}, "a"); err != nil {
		t.Fatal(err)
	}
	parent, _ := s.Run(t.Context(), NewRun("source", group.ID, "a", "message").ID)
	parent.Status = "running"
	if err := s.RunEvent(t.Context(), "run.update", &parent, nil); err != nil {
		t.Fatal(err)
	}
	id := "peer-timeout"
	text := "Hold this"
	work := Work{Trigger: "peer", DeliveryID: id, ParentRunID: &parent.ID, Message: contract.Message{
		ID: id, ThreadID: "b", Role: "user", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}},
	}}
	delivery := contract.PeerDelivery{ID: id, SourceAgentID: "a", TargetAgentID: "b", ConversationID: group.ID,
		ParentRunID: parent.ID, Intent: "request", Status: "queued", CreatedAt: Now()}
	if _, err := s.Deliver(t.Context(), delivery, work, true); err != nil {
		t.Fatal(err)
	}
	parent.Status = "completed"
	if err := s.RunEvent(t.Context(), "run.done", &parent, nil); err != nil {
		t.Fatal(err)
	}
	child, _ := s.Run(t.Context(), NewRun(id, "b", "b", "peer").ID)
	note := "The background task timed out and was stopped."
	child.Status = "failed"
	child.Error = &contract.Error{Code: "timeout", Message: note}
	if err := s.RunEvent(t.Context(), "run.done", &child, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Receipt(t.Context(), id, child, contract.Message{}); err != nil {
		t.Fatal(err)
	}
	parent, _ = s.Run(t.Context(), parent.ID)
	if parent.Status != "failed" {
		t.Fatal("parent left waiting", parent.Status)
	}
	if _, err := s.Work(t.Context(), "followup:"+parent.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("group timeout started a 1:1 follow-up", err)
	}
	saw := false
	for _, message := range mustMessages(t, s, group.ID) {
		if strings.HasPrefix(message.ID, "receipt:") || (message.AgentID != nil && *message.AgentID == "b") {
			t.Fatal("target receipt in the group", message)
		}
		if message.Role == "assistant" && message.Status == "done" && message.AgentID != nil && *message.AgentID == "a" && Text(message) == note {
			saw = true
		}
	}
	if !saw {
		t.Fatal("source agent did not explain the timeout in the group")
	}
}

func mustMessages(t *testing.T, s *Store, thread string) []contract.Message {
	t.Helper()
	messages, err := s.Messages(t.Context(), thread, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func TestSTPersistedSteer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "steer.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "A"}}); err != nil {
		t.Fatal(err)
	}
	send := func(id string) {
		t.Helper()
		request := contract.SendRequest{ID: id, Text: id, AttachmentIDs: []string{}}
		if _, _, err := s.Send(t.Context(), "a", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "a"); err != nil {
			t.Fatal(err)
		}
	}
	send("first")
	if _, err := s.Job(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	run, err := s.Run(t.Context(), NewRun("first", "a", "a", "message").ID)
	if err != nil {
		t.Fatal(err)
	}
	run.Status = "running"
	if err := s.RunEvent(t.Context(), "run.update", &run, nil); err != nil {
		t.Fatal(err)
	}
	send("ALPHA_ST")
	send("ALPHA_ST") // A retry must not duplicate the pending message.
	s.Close()
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	failed := errors.New("history unavailable")
	if err := s.DrainSteers(t.Context(), run.ID, func([]contract.Message) error { return failed }); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	persist := func(messages []contract.Message) error {
		if len(messages) != 1 || Text(messages[0]) != "ALPHA_ST" {
			t.Fatalf("messages=%v", messages)
		}
		return nil
	}
	for range 2 {
		if err := s.DrainSteers(t.Context(), run.ID, persist); err != nil {
			t.Fatal(err)
		}
	}
}
