package evals

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestVOICE2SpokenStream(t *testing.T) {
	const reply = "First, the calendar is clear. Second, the report is ready. Third, nothing else needs you today."
	h := Start(t, Options{ID: "VOICE2", Steps: []Step{Fixed(func(req *ChatRequest) error {
		if !strings.Contains(requestText(req), "Always speak") || !strings.Contains(requestText(req), "talking with the user by voice") {
			return fmt.Errorf("spoken turn lacks its voice role")
		}
		return nil
	}, TextReply(reply))}})
	body, _ := json.Marshal(contract.VoiceRequest{ID: "voice2", Text: "What's on today?"})
	response, err := http.Post(h.HTTP.URL+"/threads/"+agentID+"/voice/stream", "application/json", bytes.NewReader(body))
	if err != nil {
		h.die(err.Error())
	}
	defer response.Body.Close()
	var spoken strings.Builder
	done := false
	lines := bufio.NewScanner(response.Body)
	for lines.Scan() {
		line := lines.Text()
		if line == "event: done" {
			done = true
		}
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var event struct{ Text string }
		if json.Unmarshal([]byte(data), &event) == nil {
			spoken.WriteString(event.Text)
		}
	}
	if !done || spoken.String() != reply {
		h.die(fmt.Sprintf("spoken %q done=%v", spoken.String(), done))
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	pass(t, "VOICE2", "a spoken reply streams whole through the durable runtime")
}
