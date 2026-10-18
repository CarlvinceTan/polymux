// Package tunnel publishes the loopback API over iroh, so a paired device reaches this
// server from any network without an account. A pinned dumbpipe sidecar dials n0's public
// relays first, then upgrades to a direct hole-punched path when one exists.
package tunnel

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const version = "0.40.0"

// checksums pins each release archive; a download that does not match is never run.
var checksums = map[string]string{
	"darwin-aarch64": "69cbaf8beb0e1278a02e4c79b26507ac9fb87216c830a587e225ec59337ab30e",
	"darwin-x86_64":  "423d3bde2e480320e20dc630136bbc61b6d20f0a27e2d6c8c845c43a82df74d2",
	"linux-aarch64":  "9b5797cdfeb3d4a91575e1a7696971eec9403e400121744c88891b8ecbf2d26f",
	"linux-x86_64":   "bce08ab425dbe2642f8e89ba7e0d8e910a2da36cca662e56f793054a69f68b30",
}

// Tunnel keeps one sidecar running for the server's lifetime once started.
type Tunnel struct {
	dir    string
	target string
	log    *slog.Logger
	mu     sync.Mutex
	ticket string
	ready  chan struct{}
	start  sync.Once
	ctx    context.Context
}

// New prepares a tunnel to target (the loopback API address). It starts at once when this
// server has paired over the tunnel before, so existing devices keep working after a restart.
func New(ctx context.Context, dir, target string, log *slog.Logger) *Tunnel {
	t := &Tunnel{dir: filepath.Join(dir, "tunnel"), target: target, log: log, ready: make(chan struct{}), ctx: ctx}
	if _, err := os.Stat(filepath.Join(t.dir, "secret")); err == nil {
		t.Start()
	}
	return t
}

// Start launches the sidecar once; later calls do nothing.
func (t *Tunnel) Start() {
	t.start.Do(func() { go t.run() })
}

// Ticket starts the tunnel if needed and waits up to wait for its ticket. An empty result
// means the tunnel is not reachable yet; direct addresses still work.
func (t *Tunnel) Ticket(wait time.Duration) string {
	t.Start()
	select {
	case <-t.ready:
	case <-time.After(wait):
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ticket
}

func (t *Tunnel) run() {
	delay := 2 * time.Second
	for t.ctx.Err() == nil {
		started := time.Now()
		if err := t.serve(); err != nil && t.ctx.Err() == nil {
			t.log.Warn("tunnel unavailable", "error", err)
		}
		t.mu.Lock()
		t.ticket = ""
		t.mu.Unlock()
		if time.Since(started) > time.Minute {
			delay = 2 * time.Second
		}
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, time.Minute)
	}
}

func (t *Tunnel) serve() error {
	bin, err := Binary(t.ctx, t.dir)
	if err != nil {
		return err
	}
	secret, err := Secret(t.dir)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(t.ctx, bin, "listen-tcp", "--host", t.target)
	cmd.Env = append(os.Environ(), "IROH_SECRET="+secret)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		if ticket := ParseTicket(scanner.Text()); ticket != "" {
			t.mu.Lock()
			t.ticket = ticket
			t.mu.Unlock()
			t.markReady()
			t.log.Info("tunnel ready")
		}
	}
	return cmd.Wait()
}

func (t *Tunnel) markReady() {
	select {
	case <-t.ready:
	default:
		close(t.ready)
	}
}

// ParseTicket returns the endpoint ticket dumbpipe prints on its connect hint line.
func ParseTicket(line string) string {
	for _, field := range strings.Fields(line) {
		if strings.HasPrefix(field, "endpoint") && len(field) > len("endpoint")+32 {
			return field
		}
	}
	return ""
}

// Secret returns this server's persistent iroh key, so its ticket survives restarts.
func Secret(dir string) (string, error) {
	path := filepath.Join(dir, "secret")
	if data, err := os.ReadFile(path); err == nil {
		if secret := strings.TrimSpace(string(data)); len(secret) == 64 {
			return secret, nil
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(key)
	return secret, os.WriteFile(path, []byte(secret+"\n"), 0600)
}

// Binary finds dumbpipe: POLYMUX_DUMBPIPE, then PATH, then a verified download in dir.
func Binary(ctx context.Context, dir string) (string, error) {
	if path := os.Getenv("POLYMUX_DUMBPIPE"); path != "" {
		return path, nil
	}
	if path, err := exec.LookPath("dumbpipe"); err == nil {
		return path, nil
	}
	path := filepath.Join(dir, "dumbpipe-"+version)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return path, download(ctx, path)
}

func download(ctx context.Context, path string) error {
	arch := map[string]string{"arm64": "aarch64", "amd64": "x86_64"}[runtime.GOARCH]
	platform := runtime.GOOS + "-" + arch
	want, ok := checksums[platform]
	if !ok {
		return fmt.Errorf("no tunnel build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	url := fmt.Sprintf("https://github.com/n0-computer/dumbpipe/releases/download/v%s/dumbpipe-v%s-%s.tar.gz", version, version, platform)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 2 * time.Minute}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("tunnel download returned HTTP %d", response.StatusCode)
	}
	archive, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if err != nil {
		return err
	}
	if sum := sha256.Sum256(archive); hex.EncodeToString(sum[:]) != want {
		return errors.New("tunnel download failed its checksum")
	}
	return extract(archive, path)
}

func extract(archive []byte, path string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err != nil {
			return errors.Join(errors.New("tunnel archive has no dumbpipe binary"), err)
		}
		if filepath.Base(header.Name) != "dumbpipe" || header.Typeflag != tar.TypeReg {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		partial := path + ".partial"
		file, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, io.LimitReader(reader, 64<<20))
		if err := errors.Join(copyErr, file.Close()); err != nil {
			return err
		}
		return os.Rename(partial, path)
	}
}
