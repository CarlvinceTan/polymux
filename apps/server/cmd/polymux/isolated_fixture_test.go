//go:build verifyfixture

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsolatedFixtureServer(t *testing.T) {
	if os.Getenv("POLYMUX_FIXTURE_CHILD") != "1" {
		return
	}
	args := []string{
		"-verify-fixture", "-verify-isolated", "-data", os.Getenv("POLYMUX_FIXTURE_DATA"),
		"-listen", os.Getenv("POLYMUX_FIXTURE_LISTEN"), "-device-listen", "127.0.0.1:0",
		"-no-mdns", "-no-tunnel", "-log-dir", os.Getenv("POLYMUX_FIXTURE_LOGS"),
	}
	if err := runServer(args); err != nil {
		t.Fatal(err)
	}
}

func TestIsolatedFixtureChoice(t *testing.T) {
	var requests atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer model.Close()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestIsolatedFixtureServer$")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + root, "TMPDIR=" + os.TempDir(),
		"POLYMUX_FIXTURE_CHILD=1", "POLYMUX_FIXTURE_DATA=" + data,
		"POLYMUX_FIXTURE_LISTEN=" + address, "POLYMUX_FIXTURE_LOGS=" + filepath.Join(root, "logs"),
		"POLYMUX_MODEL_URL=" + model.URL, "POLYMUX_MODEL_KEY=fixture-only",
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		if err := cmd.Wait(); err != nil {
			t.Errorf("server shutdown: %v\n%s", err, output.String())
		}
	})
	client := &http.Client{Timeout: time.Second}
	tokenPath := filepath.Join(data, "devices", "local-token")
	var token string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		value, _ := os.ReadFile(tokenPath)
		if len(value) == 0 {
			continue
		}
		token = strings.TrimSpace(string(value))
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/health", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		token = ""
	}
	if token == "" {
		t.Fatal("fixture server did not become healthy")
	}
	url := "http://" + address + "/threads/t-approval/messages/choice:t-approval/answer"
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(`{"optionID":"day"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("choice answer: status=%d body=%s err=%v", response.StatusCode, body, err)
	}
	var result struct {
		Parts []struct {
			AnswerID string `json:"answerID"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(body, &result); err != nil || len(result.Parts) != 1 || result.Parts[0].AnswerID != "day" {
		t.Fatalf("choice was not saved: %s, %v", body, err)
	}
	// The answer queues a continuation; no model client exists even with a configured endpoint.
	time.Sleep(500 * time.Millisecond)
	if got := requests.Load(); got != 0 {
		t.Fatalf("fixture contacted a model %d times", got)
	}
}
