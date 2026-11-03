package embedproc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartRequiresBinary(t *testing.T) {
	if _, err := Start(context.Background(), Config{}); err == nil {
		t.Fatal("expected an error for an unconfigured binary")
	}
	if _, err := Start(context.Background(), Config{Bin: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("expected an error for a missing binary")
	}
}

// TestHelperLlamaServer is a stand-in for llama-server. TestStartSpawnsChild
// re-executes this test binary as the child, which then serves /health.
func TestHelperLlamaServer(t *testing.T) {
	if os.Getenv("EMBEDPROC_HELPER") != "1" {
		t.Skip("helper process")
	}
	port := ""
	for n, arg := range os.Args {
		if arg == "--port" && n+1 < len(os.Args) {
			port = os.Args[n+1]
		}
	}
	if port == "" {
		t.Fatal("the helper needs a --port argument")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})
	http.ListenAndServe("127.0.0.1:"+port, mux)
}

func TestStartSpawnsChild(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// llama.cpp flags reach the wrapper, so it pulls out --port and hands the
	// test binary a flag set it accepts.
	script := filepath.Join(t.TempDir(), "llama-server")
	body := `#!/bin/sh
port=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--port" ]; then port="$2"; fi
  shift
done
exec '` + self + `' -test.run='^TestHelperLlamaServer$' -- --port "$port"
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EMBEDPROC_HELPER", "1")
	p, err := Start(t.Context(), Config{Bin: script})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.HasSuffix(p.URL(), "/v1/embeddings") {
		t.Fatalf("url %q", p.URL())
	}
	p.Close()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("child survived Close")
	}
}

func TestStartDetectsExitedProcess(t *testing.T) {
	script := filepath.Join(t.TempDir(), "exit")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := Start(context.Background(), Config{Bin: script}); err == nil {
		t.Fatal("expected an error when the child exits immediately")
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("exit was not detected promptly: %s", elapsed)
	}
}

func TestWaitReadyPolls(t *testing.T) {
	var calls atomic.Int32
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer health.Close()
	if err := waitReady(context.Background(), health.URL, make(chan struct{})); err != nil {
		t.Fatalf("waitReady: %v", err)
	}
	if calls.Load() < 3 {
		t.Fatalf("expected retries, got %d calls", calls.Load())
	}
}

func TestWaitReadyGivesUp(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer health.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := waitReady(ctx, health.URL, make(chan struct{}))
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected an HTTP status error, got %v", err)
	}
}

func TestWaitReadyStopsOnExit(t *testing.T) {
	exited := make(chan struct{})
	close(exited)
	err := waitReady(context.Background(), "http://127.0.0.1:1/health", exited)
	if err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("expected an exit error, got %v", err)
	}
}

func TestBoundedKeepsTail(t *testing.T) {
	logs := &bounded{}
	for n := 0; n < 200; n++ {
		if _, err := logs.Write([]byte("0123456789abcdef\n")); err != nil {
			t.Fatal(err)
		}
	}
	tail := logs.tail()
	if len(tail) > 1<<13 || !strings.HasSuffix(tail, "0123456789abcdef\n") {
		t.Fatalf("unexpected tail: %d bytes", len(tail))
	}
}
