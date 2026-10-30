package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func streamVoice(t *testing.T, url, id string) string {
	t.Helper()
	body, _ := json.Marshal(contract.VoiceRequest{ID: id, Text: "What's on today?"})
	response, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d %s", response.StatusCode, response.Header.Get("Content-Type"))
	}
	data, _ := io.ReadAll(response.Body)
	return string(data)
}

func TestSpokenVoiceStreamsReply(t *testing.T) {
	f := &voiceFactory{}
	api, web := voiceTest(t, f)
	events := streamVoice(t, web.URL+"/threads/a/voice/stream", "spoken-1")
	if !strings.Contains(events, `data: {"agentID":"a","text":"Private result"}`) || !strings.Contains(events, "event: done") {
		t.Fatalf("stream %q", events)
	}
	f.mu.Lock()
	prompt := f.opened[0].Prompt
	f.mu.Unlock()
	if !strings.Contains(prompt, "talking with the user by voice") || strings.Contains(prompt, "spokesperson") {
		t.Fatal("spoken turn used the spokesperson role")
	}
	privateChat(t, api, "a")
}

func TestSpokenVoiceStaysSilentOnNoResponse(t *testing.T) {
	f := &voiceFactory{script: func(context.Context, agentruntime.Definition, contract.Message) (agentruntime.Result, error) {
		return agentruntime.Result{Text: "NO_RESPONSE"}, nil
	}}
	_, web := voiceTest(t, f)
	events := streamVoice(t, web.URL+"/threads/a/voice/stream", "spoken-2")
	if strings.Contains(events, "NO_RESPONSE") || !strings.Contains(events, "event: done") {
		t.Fatalf("stream %q", events)
	}
}

// The stream can stop early; the final reply fills in the rest, including held-back text.
func TestSpokenVoiceSpeaksWholeReply(t *testing.T) {
	for _, deltas := range [][]string{{"N"}, {"Nice "}, {}} {
		f := &voiceFactory{deltas: deltas, script: func(context.Context, agentruntime.Definition, contract.Message) (agentruntime.Result, error) {
			return agentruntime.Result{Text: "Nice work today"}, nil
		}}
		_, web := voiceTest(t, f)
		var spoken strings.Builder
		for line := range strings.Lines(streamVoice(t, web.URL+"/threads/a/voice/stream", "spoken-3")) {
			var event struct{ Text string }
			if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data: "); ok && json.Unmarshal([]byte(data), &event) == nil {
				spoken.WriteString(event.Text)
			}
		}
		if spoken.String() != "Nice work today" {
			t.Fatalf("deltas %q spoke %q", deltas, spoken.String())
		}
	}
}

func TestOpenAISessionUsesChosenVoice(t *testing.T) {
	message := map[string]any{"type": "session.update", "session": map[string]any{"instructions": "Hi"}}
	if !configureSpeech(message, contract.Settings{LiveProvider: "openai", LiveVoice: "marin"}) {
		t.Fatal("session rejected")
	}
	data, _ := json.Marshal(message)
	if !strings.Contains(string(data), `"output":{"voice":"marin"}`) {
		t.Fatalf("voice not applied: %s", data)
	}
	if configureSpeech(map[string]any{"setup": map[string]any{}}, contract.Settings{LiveProvider: "openai"}) {
		t.Fatal("a Gemini setup was accepted for OpenAI")
	}
}
