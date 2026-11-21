package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	serverName  = "hub.polymux.local"
	localUser   = "me"
	accountFile = "account.json"
)

// homeserver runs Tuwunel on loopback, with federation off.
type homeserver struct {
	binary string
	dir    string
	port   int
	secret string
	log    *slog.Logger
}

func (h *homeserver) url() string { return fmt.Sprintf("http://127.0.0.1:%d", h.port) }

func (h *homeserver) appservices() string { return filepath.Join(h.dir, "appservices") }

// config turns registration off once Hub's one account exists.
func (h *homeserver) config() string {
	_, registered := loadAccount(filepath.Join(h.dir, accountFile))
	return fmt.Sprintf(`[global]
server_name = %q
database_path = %q
address = ["127.0.0.1"]
port = %d
allow_registration = %t
registration_token = %q
new_user_displayname_suffix = ""
allow_federation = false
appservice_dir = %q
`, serverName, filepath.Join(h.dir, "db"), h.port, !registered, h.secret, h.appservices())
}

func (h *homeserver) prepare() error {
	if err := os.MkdirAll(h.appservices(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(h.dir, "tuwunel.toml"), []byte(h.config()), 0o600)
}

// run keeps Tuwunel up until ctx ends, reporting each time it answers.
func (h *homeserver) run(ctx context.Context, ready func(), down func(error)) {
	failures := 0
	for ctx.Err() == nil {
		began := time.Now()
		err := h.once(ctx, ready)
		if ctx.Err() != nil {
			return
		}
		if time.Since(began) > time.Minute {
			failures = 0
		}
		failures++
		h.log.Warn("homeserver stopped", "error", err, "failures", failures)
		if failures >= 5 {
			down(err)
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(failures*failures) * time.Second):
		}
	}
}

func (h *homeserver) once(ctx context.Context, ready func()) error {
	pidFile := filepath.Join(h.dir, "tuwunel.pid")
	reap(pidFile)
	cmd := exec.Command(h.binary, "--config", filepath.Join(h.dir, "tuwunel.toml"))
	cmd.Dir = h.dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logFile, err := os.OpenFile(filepath.Join(h.dir, "tuwunel.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	defer os.Remove(pidFile)
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	answered := make(chan struct{})
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		if answers(waitCtx, h.url()+"/_matrix/client/versions") {
			close(answered)
		}
	}()
	for {
		select {
		case err := <-exited:
			return err
		case <-answered:
			answered = nil
			ready()
		case <-ctx.Done():
			terminate(cmd.Process.Pid, exited)
			return ctx.Err()
		}
	}
}

// reap stops a child a crashed Hub left behind, which would otherwise hold its port.
func reap(pidFile string) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err == nil && pid > 1 {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
	}
	_ = os.Remove(pidFile)
}

func terminate(pid int, exited <-chan error) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-exited
	}
}

func answers(ctx context.Context, url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	for ctx.Err() == nil {
		if response, err := client.Get(url); err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return true
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(150 * time.Millisecond):
		}
	}
	return false
}

// account is Hub's one Matrix user, created on first run.
type account struct {
	UserID   string `json:"userID"`
	Token    string `json:"token"`
	Password string `json:"password"`
}

func loadAccount(path string) (account, bool) {
	var saved account
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &saved) != nil || saved.Token == "" {
		return account{}, false
	}
	return saved, true
}

func (h *homeserver) register(ctx context.Context, path string) (account, error) {
	if saved, ok := loadAccount(path); ok {
		return saved, nil
	}
	password := randomHex(24)
	body := map[string]any{"username": localUser, "password": password, "inhibit_login": false,
		"initial_device_display_name": "Polymux Hub"}
	var result struct {
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
		Session     string `json:"session"`
	}
	// The first answer is a 401 that opens the registration-token stage.
	status, data, err := postJSON(ctx, h.url()+"/_matrix/client/v3/register", body)
	if err != nil {
		return account{}, err
	}
	if status == http.StatusUnauthorized {
		if err := json.Unmarshal(data, &result); err != nil {
			return account{}, err
		}
		body["auth"] = map[string]any{"type": "m.login.registration_token", "token": h.secret, "session": result.Session}
		if status, data, err = postJSON(ctx, h.url()+"/_matrix/client/v3/register", body); err != nil {
			return account{}, err
		}
	}
	if status != http.StatusOK {
		return account{}, fmt.Errorf("register: homeserver answered %d", status)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return account{}, err
	}
	saved := account{UserID: result.UserID, Token: result.AccessToken, Password: password}
	encoded, err := json.Marshal(saved)
	if err != nil {
		return account{}, err
	}
	return saved, os.WriteFile(path, encoded, 0o600)
}

func postJSON(ctx context.Context, url string, body any) (int, []byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	var output bytes.Buffer
	if _, err := output.ReadFrom(response.Body); err != nil {
		return 0, nil, err
	}
	return response.StatusCode, output.Bytes(), nil
}

func randomHex(size int) string {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

var errNotReady = errors.New("Hub is still starting.")
