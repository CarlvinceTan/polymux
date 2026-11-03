// Package embedproc runs the local llama.cpp embedding server as a child of the
// Go server, so one process owns the whole search path.
package embedproc

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

const readyTimeout = 90 * time.Second

type Config struct {
	Bin    string // llama-server binary
	Model  string // GGUF path; empty uses llama.cpp's bundled EmbeddingGemma default
	Mmproj string // multimodal projector GGUF; without it media is never embedded
	Log    *slog.Logger
}

type Process struct {
	cmd  *exec.Cmd
	url  string
	done chan struct{}
}

// Start launches the embedding server on a private port and waits until it
// reports healthy.
func Start(ctx context.Context, cfg Config) (*Process, error) {
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
	p := &Process{cmd: cmd, url: fmt.Sprintf("http://127.0.0.1:%d/v1/embeddings", port), done: make(chan struct{})}
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

// URL is the OpenAI-compatible embeddings endpoint of the child server.
func (p *Process) URL() string { return p.url }

func (p *Process) Close() {
	if p.cmd.Process == nil {
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
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
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
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
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
