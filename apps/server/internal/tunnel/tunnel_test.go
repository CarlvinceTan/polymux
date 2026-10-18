package tunnel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

func TestExtractKeepsOnlyTheBinary(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	writer := tar.NewWriter(gz)
	for name, body := range map[string]string{"README.md": "docs", "dumbpipe-v0.40.0/dumbpipe": "#!/bin/sh\n"} {
		_ = writer.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = io.WriteString(writer, body)
	}
	_ = writer.Close()
	_ = gz.Close()
	path := filepath.Join(t.TempDir(), "bin", "dumbpipe")
	if err := extract(archive.Bytes(), path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "#!/bin/sh\n" {
		t.Fatalf("extracted %q", data)
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
