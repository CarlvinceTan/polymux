package evals

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestSCHED1CronLifecycle(t *testing.T) {
	var routineID string
	h := Start(t, Options{ID: "SCHED1", Summary: func(*ChatRequest) Response {
		return TextReply("The user scheduled recurring project updates in New York time. Process the latest request using the saved routine.")
	}, Steps: []Step{
		Fixed(nil, ToolReply("sched1-create", "manage_routines", map[string]any{
			"action": "create", "prompt": "Prepare the daily project update", "crons": []string{"0 9 * * 1-5", "0 12 * * 6,0"}, "timezone": "America/New_York",
		})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "sched1-create", `"trigger":"cron"`) || !hasToolResult(req, "sched1-create", "America/New_York") {
				return fmt.Errorf("cron creation receipt lacks mode/timezone")
			}
			return nil
		}, TextReply("I scheduled weekday morning and weekend midday updates in New York time.")),
		Fixed(func(req *ChatRequest) error {
			if !strings.Contains(requestText(req), "Prepare the daily project update") {
				return fmt.Errorf("scheduled occurrence lacks original task")
			}
			return nil
		}, TextReply("The project update is ready.")),
		{Response: func(*ChatRequest) Response {
			return ToolReply("sched1-cancel", "manage_routines", map[string]any{"action": "cancel", "id": routineID})
		}},
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "sched1-cancel", `"cancelled":true`) {
				return fmt.Errorf("cancellation failed")
			}
			return nil
		}, TextReply("I cancelled those updates.")),
	}})
	turns := func() int {
		count := 0
		for _, req := range h.Emulator.Requests() {
			if !strings.Contains(requestText(req), "You are a context summarization assistant") {
				count++
			}
		}
		return count
	}
	if code := h.Send(agentID, "sched1-user", "Schedule weekday 9am and weekend noon project updates in New York time."); code != http.StatusAccepted {
		h.die(fmt.Sprintf("create status %d", code))
	}
	if run := h.WaitRun(agentID, "sched1-user", agentID, deadline); run.Status != "completed" {
		h.die("create run " + run.Status)
	}
	routines, err := h.Store.Routines(t.Context(), agentID)
	if err != nil || len(routines) != 1 || len(routines[0].Crons) != 2 {
		h.die(fmt.Sprintf("saved cron %+v %v", routines, err))
	}
	routineID = routines[0].ID
	at, err := time.Parse(time.RFC3339, routines[0].At)
	if err != nil {
		h.die(err.Error())
	}
	if err := h.Store.Schedule(t.Context(), at); err != nil {
		h.die(err.Error())
	}
	waitRoutine(t, h, routineID)
	if len(h.AssistantDone(agentID)) != 2 {
		h.die("scheduled reply not delivered")
	}
	h.Restart()
	if turns() != 3 {
		h.die("completed occurrence replayed after restart")
	}
	if code := h.Send(agentID, "sched1-stop", "Cancel the project updates."); code != http.StatusAccepted {
		h.die(fmt.Sprintf("cancel status %d", code))
	}
	if run := h.WaitRun(agentID, "sched1-stop", agentID, deadline); run.Status != "completed" {
		h.die("cancel run " + run.Status)
	}
	if err := h.Store.Schedule(t.Context(), at.Add(7*24*time.Hour)); err != nil {
		h.die(err.Error())
	}
	h.Restart()
	if turns() != 5 {
		h.die("cancelled cron ran again")
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	pass(t, "SCHED1", "real routine tool created multi-cron zoned recurrence, delivered a scheduled update, and cancelled durably without restart replay")
}

func TestWATCH1ChangeDelivery(t *testing.T) {
	var content atomic.Value
	content.Store(`{"price":100,"fetchedAt":"first"}`)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(content.Load().(string)))
	}))
	defer endpoint.Close()
	h := Start(t, Options{ID: "WATCH1", Steps: []Step{
		Fixed(nil, ToolReply("watch1-create", "manage_routines", map[string]any{"action": "create", "prompt": "Tell me when the price changes", "interval_seconds": 60, "watch": map[string]any{"url": endpoint.URL, "jsonPointers": []string{"/price"}}})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "watch1-create", `"trigger":"watch"`) {
				return fmt.Errorf("watch creation failed")
			}
			return nil
		}, TextReply("I will check that price and notify you when it changes.")),
		Fixed(func(req *ChatRequest) error {
			text := requestText(req)
			if !strings.Contains(text, `"value":100`) || !strings.Contains(text, `"value":80`) {
				return fmt.Errorf("watch wake lacks actual before/after price")
			}
			return nil
		}, TextReply("The price dropped from 100 to 80.")),
	}})
	if code := h.Send(agentID, "watch1-user", "Watch this price and tell me when it changes."); code != http.StatusAccepted {
		h.die(fmt.Sprintf("create status %d", code))
	}
	if run := h.WaitRun(agentID, "watch1-user", agentID, deadline); run.Status != "completed" {
		h.die("watch create " + run.Status)
	}
	routines, err := h.Store.Routines(t.Context(), agentID)
	if err != nil || len(routines) != 1 || routines[0].Watch == nil {
		h.die(fmt.Sprintf("saved watch %+v %v", routines, err))
	}
	id := routines[0].ID
	pollWatch(t, h, id)
	if h.Emulator.Count() != 2 {
		h.die("baseline woke model")
	}
	content.Store(`{"fetchedAt":"later","price":100}`)
	pollWatch(t, h, id)
	if h.Emulator.Count() != 2 {
		h.die("unchanged selected price woke model")
	}
	content.Store(`{"price":80,"fetchedAt":"changed"}`)
	pollWatch(t, h, id)
	if err := h.Store.Schedule(t.Context(), time.Now()); err != nil {
		h.die(err.Error())
	}
	waitRoutine(t, h, id)
	replies := h.AssistantDone(agentID)
	if len(replies) != 2 || !strings.Contains(messageText(replies[1]), "100 to 80") {
		h.die("change notification missing")
	}
	h.Restart()
	if err := h.Store.Schedule(t.Context(), time.Now()); err != nil {
		h.die(err.Error())
	}
	if h.Emulator.Count() != 3 {
		h.die("watch notification replayed after restart")
	}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodDelete, h.HTTP.URL+"/agents/"+agentID+"/routines/"+id, nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		h.die(err.Error())
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		h.die(fmt.Sprintf("cancel status %d", response.StatusCode))
	}
	content.Store(`{"price":50}`)
	h.Restart()
	if due, err := h.Store.DueWatches(t.Context(), time.Now().Add(time.Hour)); err != nil || len(due) != 0 {
		h.die("cancelled watch still polled")
	}
	if h.Emulator.Count() != 3 {
		h.die("cancelled watch woke model")
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	pass(t, "WATCH1", "real HTTP polling established/reused a quiet baseline across restarts, ignored volatile fields, delivered one changed-price notice, and cancelled durably")
}

func pollWatch(t *testing.T, h *Harness, id string) {
	t.Helper()
	due := time.Now().UTC().Add(-time.Second)
	date := due.Format(time.RFC3339)
	if _, err := h.Store.DB.ExecContext(t.Context(), `UPDATE routines SET next_at=?,data=json_set(data,'$.nextAt',?) WHERE id=?`, due.Unix(), date, id); err != nil {
		h.die(err.Error())
	}
	h.Restart()
	until := time.Now().Add(deadline)
	for time.Now().Before(until) {
		r, err := h.Store.Routine(t.Context(), agentID, id)
		if err == nil && r.NextAt != nil {
			next, _ := time.Parse(time.RFC3339, *r.NextAt)
			if next.After(due) {
				status, err := h.Store.WatchStatus(t.Context(), agentID, id)
				if err != nil || status.Error != "" {
					h.die(fmt.Sprintf("watch check %+v %v", status, err))
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.die("native watch worker did not poll")
}

func waitRoutine(t *testing.T, h *Harness, id string) contract.Run {
	t.Helper()
	until := time.Now().Add(deadline)
	for time.Now().Before(until) {
		runs, err := h.Store.Runs(t.Context(), agentID)
		if err != nil {
			h.die(err.Error())
		}
		for _, run := range runs {
			if run.RoutineID != nil && *run.RoutineID == id {
				result := h.WaitRunID(run.ID, deadline)
				if result.Status != "completed" {
					h.die(fmt.Sprintf("routine failed %+v", result))
				}
				return result
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.die("routine run not created")
	return contract.Run{}
}
