package main

import (
	"bufio"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
)

func TestLocalServer(t *testing.T) {
	if os.Getenv("POLYMUX_TEST_SERVER") != "1" {
		return
	}
	args := []string{"-data", os.Getenv("POLYMUX_TEST_DATA"), "-listen", "127.0.0.1:0", "-device-listen", "127.0.0.1:0", "-no-mdns"}
	if err := runServer(args); err != nil {
		t.Fatal(err)
	}
}

func TestLocalStartup(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "polymux", "data")
	path := filepath.Join(home, ".polymux", "data", "devices", "local-token")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	start := func() (string, func()) {
		t.Helper()
		cmd := exec.Command(executable, "-test.run=^TestLocalServer$")
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "POLYMUX_") && !strings.HasPrefix(entry, "HOME=") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "HOME="+home, "POLYMUX_TEST_SERVER=1", "POLYMUX_TEST_DATA="+dir)
		stderr, err := cmd.StderrPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		ready := make(chan string, 1)
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.Contains(line, "Polymux is listening") {
					for _, field := range strings.Fields(line) {
						if address, ok := strings.CutPrefix(field, "address="); ok {
							ready <- "http://" + address
						}
					}
				}
			}
		}()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		stopped := false
		stop := func() {
			t.Helper()
			if stopped {
				return
			}
			stopped = true
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("server shutdown: %v", err)
				}
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				<-done
				t.Error("server did not shut down")
			}
		}
		t.Cleanup(stop)
		select {
		case url := <-ready:
			return url, stop
		case err := <-done:
			stopped = true
			t.Fatalf("server exited before listening: %v", err)
		case <-time.After(30 * time.Second):
			t.Fatal("server did not start")
		}
		return "", stop
	}
	readToken := func() string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	check := func(url, token string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url+"/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		client := &http.Client{Timeout: 5 * time.Second}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("HTTP status %d, want %d", response.StatusCode, want)
		}
		t.Logf("client-path credential present=%t: HTTP %d", token != "", response.StatusCode)
	}
	url, stop := start()
	token := readToken()
	check(url, token, 200)
	check(url, "", 401)
	check(url, strings.Repeat("x", 43), 401)
	pairs, err := device.Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := pairs.Authenticate(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if err := pairs.Revoke(t.Context(), pair.ID); err != nil {
		t.Fatal(err)
	}
	if err := pairs.Close(); err != nil {
		t.Fatal(err)
	}
	check(url, token, 401)
	stop()
	url, _ = start()
	replacement := readToken()
	if replacement == token {
		t.Fatal("restart reused revoked credential")
	}
	check(url, replacement, 200)
	check(url, token, 401)
	check(url, "", 401)
}
