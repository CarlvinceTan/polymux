// Package acptest serves a scripted Agent Client Protocol agent for tests.
//
// A test binary re-executes itself with Env set, so tests of the ACP client and of
// the server's external-agent turns run against a real stdio process without an
// extra build step.
package acptest

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// Env makes a test binary act as the fake agent instead of running tests.
const Env = "POLYMUX_ACP_TEST_AGENT"

// Created is the session ID the fake reports from session/new.
const Created = "fake-session"

// Crash in a prompt makes the fake exit mid-turn.
const Crash = "fake-agent-crash"

// Wait in a prompt makes the fake stream one chunk, then hold the turn until session/cancel.
const Wait = "fake-agent-wait"

// Serve answers ACP requests on stdin until the parent closes it.
func Serve() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1<<20)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		var msg struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(in.Bytes(), &msg) != nil || msg.Method == "" {
			continue
		}
		switch msg.Method {
		case "initialize":
			respond(out, msg.ID, map[string]any{
				"protocolVersion":   1,
				"agentCapabilities": map[string]any{"loadSession": true},
			})
		case "session/new":
			respond(out, msg.ID, map[string]any{"sessionId": Created})
		case "session/load":
			var params struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			if params.SessionID != Created && params.SessionID != "known" {
				respondError(out, msg.ID, -32002, "unknown session")
				continue
			}
			respond(out, msg.ID, map[string]any{})
		case "session/prompt":
			var params struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			wait := false
			for _, block := range params.Prompt {
				if strings.Contains(block.Text, Crash) {
					os.Exit(1)
				}
				wait = wait || strings.Contains(block.Text, Wait)
			}
			if wait {
				hold(out, in, msg.ID)
			} else {
				prompt(out, in, msg.ID)
			}
		}
	}
}

func prompt(out *json.Encoder, in *bufio.Scanner, id *int64) {
	update(out, map[string]any{"sessionUpdate": "agent_message_chunk",
		"content": map[string]any{"type": "text", "text": "Reading the notes."}})
	update(out, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1",
		"title": "Read file", "rawInput": map[string]any{"path": "notes.md"}})
	update(out, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed",
		"content": []any{map[string]any{"type": "content",
			"content": map[string]any{"type": "text", "text": "notes"}}}})
	chosen := askPermission(out, in)
	text := "Hello from ACP"
	if chosen == "allow-once" {
		text += " [allow-once]"
	}
	update(out, map[string]any{"sessionUpdate": "agent_message_chunk",
		"content": map[string]any{"type": "text", "text": text}})
	respond(out, id, map[string]any{"stopReason": "end_turn"})
}

// hold streams a chunk, then answers the prompt as cancelled once session/cancel arrives.
func hold(out *json.Encoder, in *bufio.Scanner, id *int64) {
	update(out, map[string]any{"sessionUpdate": "agent_message_chunk",
		"content": map[string]any{"type": "text", "text": "Working"}})
	for in.Scan() {
		var msg struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(in.Bytes(), &msg) == nil && msg.Method == "session/cancel" {
			respond(out, id, map[string]any{"stopReason": "cancelled"})
			return
		}
	}
}

// askPermission sends a permission request and returns the selected option ID.
func askPermission(out *json.Encoder, in *bufio.Scanner) string {
	const id = 900
	_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": "session/request_permission",
		"params": map[string]any{"options": []any{
			map[string]any{"optionId": "reject", "kind": "reject_once"},
			map[string]any{"optionId": "allow-once", "kind": "allow_once"},
		}}})
	for in.Scan() {
		var response struct {
			ID     *int64 `json:"id"`
			Result struct {
				Outcome struct {
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			} `json:"result"`
		}
		if json.Unmarshal(in.Bytes(), &response) != nil || response.ID == nil || *response.ID != id {
			continue
		}
		return response.Result.Outcome.OptionID
	}
	return ""
}

func update(out *json.Encoder, update map[string]any) {
	_ = out.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update",
		"params": map[string]any{"update": update}})
}

func respond(out *json.Encoder, id *int64, result any) {
	_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func respondError(out *json.Encoder, id *int64, code int, message string) {
	_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message}})
}
