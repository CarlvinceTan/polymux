package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain doubles as a fake `cua-driver mcp` when FAKE_CUA is set.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_CUA") != "" {
		fakeDriver()
		return
	}
	os.Exit(m.Run())
}

func fakeDriver() {
	lines := bufio.NewScanner(os.Stdin)
	answer := func(id any, result any) {
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		fmt.Println(string(data))
	}
	for lines.Scan() {
		var m struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.Unmarshal(lines.Bytes(), &m)
		switch m.Method {
		case "initialize":
			answer(m.ID, map[string]any{"instructions": "Read narrowly.", "capabilities": map[string]any{"tools": map[string]any{}}})
		case "tools/list":
			answer(m.ID, map[string]any{"tools": []map[string]any{
				{"name": "get_window_state", "description": "Snapshot a window. More detail here.", "inputSchema": map[string]any{"type": "object"}},
				{"name": "set_config", "description": "Change the driver.", "inputSchema": map[string]any{"type": "object"}},
			}})
		case "tools/call":
			switch m.Params.Name {
			case "crash":
				os.Exit(1)
			case "get_window_state":
				answer(m.ID, map[string]any{"content": []map[string]any{
					{"type": "text", "text": fmt.Sprintf("pid %v pid-of-process %d", m.Params.Arguments["pid"], os.Getpid())},
					{"type": "image", "data": "aGk=", "mimeType": "image/png"},
				}, "structuredContent": map[string]any{"pid": m.Params.Arguments["pid"]}})
			default:
				answer(m.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "no such tool"}}, "isError": true})
			}
		}
	}
}

func testService(t *testing.T) http.Handler {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "cua-driver")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nFAKE_CUA=1 exec "+binary+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &service{driver: &driver{command: script}, log: slog.Default()}
	t.Cleanup(s.driver.close)
	return s.handler("secret")
}

func ask(t *testing.T, h http.Handler, tool, args string) toolResult {
	t.Helper()
	request := httptest.NewRequest("POST", "/tools/"+tool, bytes.NewBufferString(`{"chat":"c","agent":"a","args":`+args+`}`))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("%s: status %d", tool, response.Code)
	}
	var result toolResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDescribeHidesBlockedTools(t *testing.T) {
	h := testService(t)
	listed := ask(t, h, "describe", `{}`)
	if !strings.Contains(listed.Text, "Read narrowly.") || !strings.Contains(listed.Text, "- get_window_state: Snapshot a window.") || strings.Contains(listed.Text, "set_config") {
		t.Fatalf("list %q", listed.Text)
	}
	if one := ask(t, h, "describe", `{"tool":"get_window_state"}`); !strings.Contains(one.Text, "More detail here.") {
		t.Fatalf("one %q", one.Text)
	}
	if hidden := ask(t, h, "describe", `{"tool":"set_config"}`); !strings.HasPrefix(hidden.Text, "No tool named") {
		t.Fatalf("hidden %q", hidden.Text)
	}
}

// Calls return the driver's text and pictures, blocked tools never reach it, and a driver that
// exits is started again on the next call.
func TestCallPassesPicturesAndRestarts(t *testing.T) {
	h := testService(t)
	first := ask(t, h, "call", `{"tool":"get_window_state","args":{"pid":7}}`)
	if !strings.HasPrefix(first.Text, "pid 7 ") || len(first.Images) != 1 || first.Images[0].MimeType != "image/png" || string(first.Structured) != `{"pid":7}` {
		t.Fatalf("first %+v", first)
	}
	if refused := ask(t, h, "call", `{"tool":"set_config","args":{}}`); !strings.Contains(refused.Text, "doesn't offer it") {
		t.Fatalf("refused %q", refused.Text)
	}
	if failed := ask(t, h, "call", `{"tool":"nope"}`); failed.Text != "Error: no such tool" {
		t.Fatalf("failed %q", failed.Text)
	}
	if crashed := ask(t, h, "call", `{"tool":"crash"}`); !strings.Contains(crashed.Text, "stopped") {
		t.Fatalf("crashed %q", crashed.Text)
	}
	again := ask(t, h, "call", `{"tool":"get_window_state","args":{"pid":8}}`)
	if !strings.HasPrefix(again.Text, "pid 8 ") || strings.Fields(again.Text)[3] == strings.Fields(first.Text)[3] {
		t.Fatalf("again %q after %q", again.Text, first.Text)
	}
}

func TestRefusesWithoutToken(t *testing.T) {
	request := httptest.NewRequest("POST", "/tools/describe", bytes.NewBufferString(`{}`))
	response := httptest.NewRecorder()
	testService(t).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", response.Code)
	}
}
