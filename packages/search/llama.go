package search

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

const llamaReadyTimeout = 90 * time.Second

type llamaConfig struct {
	Bin    string
	Model  string
	Mmproj string
	Log    *slog.Logger
}

type llamaProcess struct {
	cmd  *exec.Cmd
	url  string
	done chan struct{}
}

func startLlama(ctx context.Context, cfg llamaConfig) (*llamaProcess, error) {
	if cfg.Bin == "" {
		return nil, fmt.Errorf("embedding server binary is not configured")
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	args := []string{"--host", "127.0.0.1", "--port", strconv.Itoa(port), "--embeddings"}
	if cfg.Model != "" {
		args = append(args, "-m", cfg.Model)
	} else {
		args = append(args, "--embd-gemma-default")
	}
	if cfg.Mmproj != "" {
		args = append(args, "--mmproj", cfg.Mmproj)
	}
	cmd := exec.Command(cfg.Bin, args...)
	logs := &bounded{}
	cmd.Stdout, cmd.Stderr = io.Discard, logs
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start embedding server: %w", err)
	}
	p := &llamaProcess{cmd: cmd, url: fmt.Sprintf("http://127.0.0.1:%d/v1/embeddings", port), done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	if err := waitReady(ctx, fmt.Sprintf("http://127.0.0.1:%d/health", port), p.done); err != nil {
		p.Close()
		return nil, fmt.Errorf("embedding server did not become healthy: %w (%s)", err, logs.tail())
	}
	if cfg.Log != nil {
		cfg.Log.Info("embedding server ready", "url", p.url)
	}
	return p, nil
}

func (p *llamaProcess) URL() string { return p.url }

func (p *llamaProcess) Close() {
	if p == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

func waitReady(ctx context.Context, url string, exited <-chan struct{}) error {
	ctx, cancel := context.WithTimeout(ctx, llamaReadyTimeout)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		res, requestErr := client.Do(req)
		if requestErr == nil {
			io.Copy(io.Discard, io.LimitReader(res.Body, 1<<12))
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-exited:
			return fmt.Errorf("process exited before it was ready")
		default:
		}
		select {
		case <-exited:
			return fmt.Errorf("process exited before it was ready")
		case <-ctx.Done():
			if requestErr != nil {
				return requestErr
			}
			return fmt.Errorf("health check returned HTTP %d", res.StatusCode)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// bounded keeps the tail of the child's log for error reporting instead of
// blocking it on an unread pipe.
type bounded struct {
	buf bytes.Buffer
}

func (b *bounded) Write(p []byte) (int, error) {
	b.buf.Write(p)
	if b.buf.Len() > 1<<13 {
		b.buf.Next(b.buf.Len() - 1<<12)
	}
	return len(p), nil
}

func (b *bounded) tail() string { return b.buf.String() }
