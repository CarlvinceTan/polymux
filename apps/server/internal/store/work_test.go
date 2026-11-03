package store

import (
	"encoding/json"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestWorkListsNewestAndLeavesOutToolOutput(t *testing.T) {
	data := openTest(t)
	if _, err := data.DB.ExecContext(t.Context(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	output := "secret output"
	for _, id := range []string{"first", "second", "third"} {
		encoded, err := json.Marshal(contract.Run{ID: id, ConversationID: "chat", AgentID: "a",
			Tools: []contract.ToolActivity{{ID: "t", RunID: id, Name: "bash", Status: contract.ToolActivityStatusCompleted, Output: &output}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := data.DB.ExecContext(t.Context(), `INSERT INTO runs(id,job_id,thread_id,agent_id,status,data) VALUES(?,?,?,?,?,?)`, id, "j", "chat", "a", "done", string(encoded)); err != nil {
			t.Fatal(err)
		}
		delivery, err := json.Marshal(contract.PeerDelivery{ID: id, SourceAgentID: "a", TargetAgentID: "b", ConversationID: "chat"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := data.DB.ExecContext(t.Context(), `INSERT INTO deliveries(id,source_agent,target_agent,thread_id,data,input) VALUES(?,?,?,?,?,?)`, id, "a", "b", "chat", string(delivery), "{}"); err != nil {
			t.Fatal(err)
		}
	}
	work, err := data.RecentWork(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(work.Runs) != 2 || work.Runs[0].ID != "third" || work.Runs[1].ID != "second" {
		t.Fatalf("runs %+v", work.Runs)
	}
	if work.Runs[0].Tools[0].Output != nil {
		t.Fatal("tool output reached the list")
	}
	if len(work.Deliveries) != 2 || work.Deliveries[0].ID != "third" {
		t.Fatalf("deliveries %+v", work.Deliveries)
	}
}
