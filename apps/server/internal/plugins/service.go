package plugins

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	stopTimeout  = 10 * time.Second
	readyTimeout = 30 * time.Second
	healthyAfter = time.Minute
	restartLimit = 5
)

var restartDelays = []time.Duration{time.Second, 5 * time.Second, 15 * time.Second, time.Minute}

// service supervises one plugin's process and forwards its API.
type service struct {
	name    string
	binary  string
	root    string
	data    string
	log     *slog.Logger
	failed  func(string)
	running func()
	// env adds to the process's environment, such as its settings file and the addresses it may publish.
	env []string

	mu      sync.Mutex
	proxy   *httputil.ReverseProxy
	address string
	token   string
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{}
}

func (s *service) start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	go s.supervise(ctx)
}

func (s *service) supervise(ctx context.Context) {
	defer close(s.done)
	failures := 0
	for {
		began := time.Now()
		err := s.run(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(began) > healthyAfter {
			failures = 0
		}
		failures++
		s.log.Warn("plugin service stopped", "plugin", s.name, "error", err, "failures", failures)
		if failures >= restartLimit {
			s.failed("The plugin stopped working.")
			return
		}
		delay := restartDelays[min(failures-1, len(restartDelays)-1)]
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (s *service) run(ctx context.Context) error {
	port, err := freePort()
	if err != nil {
		return err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	token := hex.EncodeToString(secret)
	if err := os.MkdirAll(s.data, 0o700); err != nil {
		return err
	}
	cmd := exec.Command(s.binary)
	cmd.Dir = s.root
	// Plugin processes get their own settings, not the server's credentials.
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"), "TMPDIR=" + os.TempDir(),
		"POLYMUX_PLUGIN_PORT=" + fmt.Sprint(port), "POLYMUX_PLUGIN_TOKEN=" + token,
		"POLYMUX_PLUGIN_DATA=" + s.data, "POLYMUX_PLUGIN_ROOT=" + s.root}
	cmd.Env = append(cmd.Env, s.env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logFile, err := os.OpenFile(filepath.Join(s.data, "service.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	address := fmt.Sprintf("127.0.0.1:%d", port)
	ready := make(chan error, 1)
	waitCtx, stopWait := context.WithCancel(ctx)
	defer stopWait()
	go func() { ready <- waitHealthy(waitCtx, address, token) }()
	select {
	case err := <-exited:
		return err
	case <-ctx.Done():
		stop(cmd, exited)
		return ctx.Err()
	case err := <-ready:
		if err != nil {
			stop(cmd, exited)
			return err
		}
	}
	s.publish(address, token, cmd)
	s.running()
	defer s.publish("", "", nil)
	select {
	case err := <-exited:
		return err
	case <-ctx.Done():
		stop(cmd, exited)
		return ctx.Err()
	}
}

func (s *service) publish(address, token string, cmd *exec.Cmd) {
	var proxy *httputil.ReverseProxy
	if address != "" {
		target := &url.URL{Scheme: "http", Host: address}
		proxy = &httputil.ReverseProxy{
			FlushInterval: -1,
			Rewrite: func(request *httputil.ProxyRequest) {
				request.SetURL(target)
				request.Out.Header.Set("Authorization", "Bearer "+token)
				request.Out.Header.Del("Cookie")
			},
			ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
				http.Error(w, `{"code":"unavailable","message":"The plugin isn't responding."}`, http.StatusBadGateway)
			},
		}
	}
	s.mu.Lock()
	s.proxy, s.address, s.token, s.cmd = proxy, address, token, cmd
	s.mu.Unlock()
}

// get reads a JSON answer from the running service.
func (s *service) get(ctx context.Context, path string, out any) error {
	return s.call(ctx, http.MethodGet, path, nil, out)
}

// post sends JSON to the running service and reads its answer.
func (s *service) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return s.call(ctx, http.MethodPost, path, bytes.NewReader(body), out)
}

func (s *service) call(ctx context.Context, method, path string, body io.Reader, out any) error {
	s.mu.Lock()
	address, token := s.address, s.token
	s.mu.Unlock()
	if address == "" {
		return errors.New("service isn't running")
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://"+address+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("service answered %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(out)
}

func (s *service) forward(w http.ResponseWriter, r *http.Request, path string) bool {
	s.mu.Lock()
	proxy := s.proxy
	s.mu.Unlock()
	if proxy == nil {
		return false
	}
	forwarded := r.Clone(r.Context())
	forwarded.URL.Path = "/" + strings.TrimPrefix(path, "/")
	forwarded.URL.RawPath = ""
	proxy.ServeHTTP(w, forwarded)
	return true
}

func (s *service) stop() {
	if s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
}

func stop(cmd *exec.Cmd, exited <-chan error) {
	terminate := func(signal syscall.Signal) { _ = syscall.Kill(-cmd.Process.Pid, signal) }
	terminate(syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(stopTimeout):
		terminate(syscall.SIGKILL)
		<-exited
	}
}

func waitHealthy(ctx context.Context, address, token string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/health", nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		if response, err := client.Do(request); err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("service did not become healthy")
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
