package evals

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestSEC1CredentialRestart(t *testing.T) {
	const key = "isolated-eval-credential-value"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "missing credential", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"connected":true}`)
	}))
	defer provider.Close()
	h := Start(t, Options{ID: "SEC1", Summary: func(req *ChatRequest) Response {
		if strings.Contains(requestText(req), key) {
			t.Error("credential leaked into a summary request")
		}
		return TextReply("The service credential was saved through a private credential card. Use the named service credential for HTTP requests without exposing its value.")
	}, Steps: []Step{
		Fixed(nil, ToolReply("secret-card", "request_secret", map[string]any{"name": "service", "label": "Service API key", "origin": provider.URL})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "secret-card", "awaiting_user") {
				return fmt.Errorf("credential card was not posted")
			}
			return nil
		}, TextReply("Please use the credential card.")),
		Fixed(nil, ToolReply("secret-http", "http_request", map[string]any{"url": provider.URL, "credential": "service"})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "secret-http", `connected`) || strings.Contains(requestText(req), key) {
				return fmt.Errorf("credential request failed or exposed the value")
			}
			return nil
		}, TextReply("Service connected.")),
		Fixed(nil, ToolReply("secret-restarted", "http_request", map[string]any{"url": provider.URL, "credential": "service"})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "secret-restarted", `connected`) || strings.Contains(requestText(req), key) {
				return fmt.Errorf("credential did not survive restart privately")
			}
			return nil
		}, TextReply("Service still connected.")),
	}})
	if code := h.Send(agentID, "secret-user", "Connect the service using a credential card."); code != http.StatusAccepted {
		h.die(fmt.Sprintf("request status %d", code))
	}
	if run := h.WaitRun(agentID, "secret-user", agentID, deadline); run.Status != "completed" {
		h.die("request failed")
	}
	var card contract.Message
	for _, message := range h.Messages(agentID) {
		if len(message.Parts) == 1 && message.Parts[0].Kind == "credential" {
			card = message
		}
	}
	if card.ID == "" {
		h.die("credential card missing")
	}
	body, _ := json.Marshal(contract.CredentialAnswer{Value: new(key), Skip: false})
	response, err := http.Post(h.HTTP.URL+"/threads/"+agentID+"/messages/"+card.ID+"/credential", "application/json", strings.NewReader(string(body)))
	if err != nil {
		h.die(err.Error())
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		h.die(fmt.Sprintf("credential save status %d", response.StatusCode))
	}
	if run := h.WaitRun(agentID, "credential-answer:"+card.ID, agentID, deadline); run.Status != "completed" {
		h.die("credential continuation failed")
	}
	h.Restart()
	if code := h.Send(agentID, "secret-after", "Verify the service again."); code != http.StatusAccepted {
		h.die(fmt.Sprintf("restart request status %d", code))
	}
	if run := h.WaitRun(agentID, "secret-after", agentID, deadline); run.Status != "completed" {
		h.die("restart credential use failed")
	}
	for _, message := range h.Messages(agentID) {
		data, _ := json.Marshal(message)
		if strings.Contains(string(data), key) {
			h.die("credential leaked into chat")
		}
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	pass(t, "SEC1", "masked credential API saves outside chat, resumes native API work, and survives restart")
}
