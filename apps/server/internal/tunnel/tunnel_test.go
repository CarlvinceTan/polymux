package tunnel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseTicket(t *testing.T) {
	ticket := "endpointadnkkyumc3uj2hbdvgmgmktam27hmr4wo6agoxhgmlnrgdjplx3xgay"
	if got := ParseTicket("dumbpipe connect-tcp " + ticket); got != ticket {
		t.Fatalf("ticket %q", got)
	}
	for _, line := range []string{"Forwarding incoming requests to '127.0.0.1:7337'.", "To connect, use e.g.:", "endpoint"} {
		if got := ParseTicket(line); got != "" {
			t.Fatalf("%q parsed as ticket %q", line, got)
		}
	}
}

func TestSecretPersists(t *testing.T) {
	dir := t.TempDir()
	first, err := Secret(dir)
	if err != nil || len(first) != 64 {
		t.Fatalf("secret %q %v", first, err)
	}
	if again, _ := Secret(dir); again != first {
		t.Fatal("secret changed between starts, so paired tickets would break")
	}
	if info, err := os.Stat(filepath.Join(dir, "secret")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("secret file mode %v %v", info.Mode(), err)
	}
}

func release(t *testing.T) []byte {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	writer := tar.NewWriter(gz)
	for name, body := range map[string]string{"README.md": "docs", "dumbpipe-v0.40.0/dumbpipe": "#!/bin/sh\n"} {
		_ = writer.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = io.WriteString(writer, body)
	}
	_ = writer.Close()
	_ = gz.Close()
	return archive.Bytes()
}

func TestExtractKeepsOnlyTheBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin", "dumbpipe")
	if err := extract(release(t), path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "#!/bin/sh\n" {
		t.Fatalf("extracted %q", data)
	}
}

func TestInstallRejectsUnpinnedArchive(t *testing.T) {
	archive := release(t)
	path := filepath.Join(t.TempDir(), "dumbpipe")
	if err := install(archive, strings.Repeat("0", 64), path); err == nil {
		t.Fatal("installed an archive that does not match the pinned checksum")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected archive left a binary: %v", err)
	}
	sum := sha256.Sum256(archive)
	if err := install(archive, hex.EncodeToString(sum[:]), path); err != nil {
		t.Fatal(err)
	}
}

// A paired server restarts its sidecar on boot, with its saved secret, and serves the printed ticket.
func TestTicketFromSidecar(t *testing.T) {
	ticket := "endpoint" + strings.Repeat("a", 40)
	dir := t.TempDir()
	seen := filepath.Join(dir, "secret-seen")
	fake := filepath.Join(dir, "dumbpipe")
	script := fmt.Sprintf("#!/bin/sh\nprintf %%s \"$IROH_SECRET\" > %q\necho \"dumbpipe connect-tcp %s\" >&2\nexec sleep 30\n", seen, ticket)
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POLYMUX_DUMBPIPE", fake)
	secret, err := Secret(filepath.Join(dir, "tunnel"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	served := New(ctx, dir, "127.0.0.1:1", slog.New(slog.NewTextHandler(io.Discard, nil)))
	select {
	case <-served.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("a paired tunnel did not start on boot")
	}
	if got := served.Ticket(0); got != ticket {
		t.Fatalf("ticket %q", got)
	}
	if data, _ := os.ReadFile(seen); string(data) != secret {
		t.Fatalf("sidecar got secret %q, want the saved one", data)
	}
}

// Runs a real tunnel through n0's relays when a dumbpipe binary is available.
func TestTunnelForwardsHTTP(t *testing.T) {
	bin := os.Getenv("POLYMUX_DUMBPIPE")
	if bin == "" {
		t.Skip("set POLYMUX_DUMBPIPE to run a live tunnel")
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "through") }))
	defer backend.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dir := t.TempDir()
	served := New(ctx, dir, backend.Listener.Addr().String(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ticket := served.Ticket(30 * time.Second)
	if ticket == "" {
		t.Fatal("tunnel produced no ticket")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	local := listener.Addr().String()
	listener.Close()
	client := exec.CommandContext(ctx, bin, "connect-tcp", "--addr", local, ticket)
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	defer client.Process.Kill()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		response, err := http.Get("http://" + local)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if string(body) == "through" {
			if again := New(ctx, dir, backend.Listener.Addr().String(), served.log).Ticket(30 * time.Second); again == "" {
				t.Fatal("a restarted tunnel did not come back")
			}
			return
		}
	}
	t.Fatal("request never crossed the tunnel")
}
