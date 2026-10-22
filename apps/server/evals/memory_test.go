package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestMEM1MemoryAfterRestart(t *testing.T) {
	const fact = "My preferred report format is three concise paragraphs."
	h := Start(t, Options{ID: "MEM1", Summary: func(*ChatRequest) Response {
		return TextReply("The user saved a report-format preference and asks to retrieve it. Use recall_memory to recover the preference and its source.")
	}, Steps: []Step{
		Fixed(nil, ToolReply("mem1-save", "save_memory", map[string]any{"name": "report-format", "text": fact, "revision": ""})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "mem1-save", fact) || !hasToolResult(req, "mem1-save", `"revision"`) {
				return fmt.Errorf("saved memory receipt lacks fact or revision")
			}
			return nil
		}, TextReply("I will remember that report format.")),
		Fixed(nil, ToolReply("mem1-recall", "recall_memory", map[string]any{"query": "report-format"})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "mem1-recall", fact) || !hasToolResult(req, "mem1-recall", "mem1-user") {
				return fmt.Errorf("restart recall lacks original fact or source evidence")
			}
			return nil
		}, TextReply(fact)),
	}})
	if code := h.Send(agentID, "mem1-user", "Remember this preference: "+fact); code != http.StatusAccepted {
		h.die(fmt.Sprintf("save request status %d", code))
	}
	if run := h.WaitRun(agentID, "mem1-user", agentID, deadline); run.Status != "completed" {
		h.die("save run status " + run.Status)
	}
	h.Restart()
	if code := h.Send(agentID, "mem1-question", "What report format do I prefer?"); code != http.StatusAccepted {
		h.die(fmt.Sprintf("recall request status %d", code))
	}
	if run := h.WaitRun(agentID, "mem1-question", agentID, deadline); run.Status != "completed" {
		h.die("recall run status " + run.Status)
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	replies := h.AssistantDone(agentID)
	if len(replies) != 2 || messageText(replies[1]) != fact {
		h.die("recalled fact did not reach the typed reply")
	}
	pass(t, "MEM1", "saved preference and its source recalled through real tools after server/runtime restart")
}

func TestVOICE1PrivateRecall(t *testing.T) {
	const history = "The Kestrel project review is on Thursday."
	const preference = "For Kestrel reviews, use a short checklist."
	const spoken = "My private voice note is to bring the blue folder."
	h := Start(t, Options{ID: "VOICE1", Summary: func(*ChatRequest) Response {
		return TextReply("The user discussed the Kestrel review and asks about a remembered folder. Use saved memory for the answer; private consultations do not belong in typed chat.")
	}, Agents: []agentruntime.Definition{{
		Agent: contract.Agent{ID: agentID, Name: "Assistant", Model: evalModel}, Tools: []string{},
	}}, Steps: []Step{
		Fixed(nil, ToolReply("voice1-seed", "save_memory", map[string]any{"name": "kestrel-style", "text": preference, "revision": ""})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "voice1-seed", preference) {
				return fmt.Errorf("initial memory did not save")
			}
			return nil
		}, TextReply("The Kestrel review is noted.")),
		Fixed(func(req *ChatRequest) error {
			for _, name := range []string{"recall_history", "recall_memory", "save_memory"} {
				if !strings.Contains(string(req.Tools), name) {
					return fmt.Errorf("private voice is missing tool %s", name)
				}
			}
			return nil
		}, ToolReply("voice1-history", "recall_history", map[string]any{"limit": 4})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "voice1-history", history) || !hasToolResult(req, "voice1-history", "voice1-user") {
				return fmt.Errorf("private voice could not retrieve typed history")
			}
			return nil
		}, ToolReply("voice1-memory", "recall_memory", map[string]any{"query": "kestrel-style"})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "voice1-memory", preference) {
				return fmt.Errorf("private voice could not retrieve typed memory")
			}
			return nil
		}, TextReply("Thursday; use a short checklist.")),
		Fixed(nil, ToolReply("voice1-save", "save_memory", map[string]any{"name": "blue-folder", "text": spoken, "revision": ""})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "voice1-save", spoken) {
				return fmt.Errorf("private voice could not save its memory")
			}
			return nil
		}, TextReply("I will remember the blue folder.")),
		Fixed(nil, ToolReply("voice1-after", "recall_memory", map[string]any{"query": "blue-folder"})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "voice1-after", spoken) {
				return fmt.Errorf("typed conversation could not retrieve the voice-saved memory")
			}
			return nil
		}, TextReply("Bring the blue folder.")),
	}})
	if code := h.Send(agentID, "voice1-user", history+" Remember this preference: "+preference); code != http.StatusAccepted {
		h.die(fmt.Sprintf("seed request status %d", code))
	}
	if run := h.WaitRun(agentID, "voice1-user", agentID, deadline); run.Status != "completed" {
		h.die("seed run status " + run.Status)
	}
	h.Restart()
	before, err := h.Store.Thread(t.Context(), agentID)
	if err != nil {
		h.die(err.Error())
	}
	messages := h.Messages(agentID)
	voice := privateVoice(t, h, "voice1-recall", "What did we discuss about Kestrel?")
	if !strings.Contains(voice.Text, "Thursday; use a short checklist.") {
		h.die(fmt.Sprintf("private voice response %+v", voice))
	}
	after, err := h.Store.Thread(t.Context(), agentID)
	if err != nil {
		h.die(err.Error())
	}
	if len(h.Messages(agentID)) != len(messages) || before.LastSeq != after.LastSeq || before.UnreadCount != after.UnreadCount {
		h.die("private consultation altered the typed transcript or unread state")
	}
	voice = privateVoice(t, h, "voice1-save-call", "Remember this: "+spoken)
	if !strings.Contains(voice.Text, "remember the blue folder") || len(h.Messages(agentID)) != len(messages) {
		h.die("explicit voice memory save failed or published a private transcript")
	}
	if code := h.Send(agentID, "voice1-question", "What did I ask you to remember about the folder?"); code != http.StatusAccepted {
		h.die(fmt.Sprintf("typed recall request status %d", code))
	}
	if run := h.WaitRun(agentID, "voice1-question", agentID, deadline); run.Status != "completed" {
		h.die("typed recall run status " + run.Status)
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	pass(t, "VOICE1", "private voice retrieved typed history/memory and saved a fact for typed recall without publishing the consultation")
}

func privateVoice(t *testing.T, h *Harness, id, text string) contract.VoiceResponse {
	t.Helper()
	body, err := json.Marshal(contract.VoiceRequest{ID: id, Text: text})
	if err != nil {
		h.die(err.Error())
	}
	ctx, cancel := context.WithTimeout(t.Context(), deadline)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, h.HTTP.URL+"/threads/"+agentID+"/voice", bytes.NewReader(body))
	if err != nil {
		h.die(err.Error())
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		h.die(err.Error())
	}
	defer response.Body.Close()
	var result contract.VoiceResponse
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&result) != nil {
		h.die(fmt.Sprintf("private voice request status=%d", response.StatusCode))
	}
	return result
}
