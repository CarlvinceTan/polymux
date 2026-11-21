package acp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeRegistry serves agents as the ACP registry and resolves commands from installed.
func fakeRegistry(t *testing.T, installed []string, agents ...map[string]any) *Registry {
	t.Helper()
	data, err := json.Marshal(map[string]any{"version": "1", "agents": agents})
	if err != nil {
		t.Fatal(err)
	}
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	t.Cleanup(cdn.Close)
	r := NewRegistry(t.TempDir(), cdn.URL)
	r.look = func(name string) (string, error) {
		if slices.Contains(installed, name) {
			return "/usr/local/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	if err := r.refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r
}

func npxAgent(id, pkg string) map[string]any {
	return map[string]any{"id": id, "name": id, "version": "1", "description": "",
		"distribution": map[string]any{"npx": map[string]any{"package": pkg, "args": []string{"--acp"}, "env": map[string]string{"NO_UPDATE": "1"}}}}
}

func binaryAgent(id string, b map[string]any) map[string]any {
	return map[string]any{"id": id, "name": id, "version": "1", "description": "",
		"distribution": map[string]any{"binary": map[string]any{platform(): b}}}
}

func TestDetectedFindsInstalledCommands(t *testing.T) {
	r := fakeRegistry(t, []string{"claude", "goose", "code"},
		npxAgent("claude-acp", "@agentclientprotocol/claude-agent-acp@1.0.0"),
		binaryAgent("goose", map[string]any{"archive": "https://example.invalid/g.tar.gz", "cmd": "./goose"}),
		npxAgent("minimax-code", "@minimax-ai/code@0.2.7"),
		npxAgent("gemini", "@google/gemini-cli@1.0.0"),
	)
	var ids []string
	for _, agent := range r.Detected() {
		ids = append(ids, agent.ID)
	}
	// A generic package name such as code doesn't count as minimax-code being installed.
	if !slices.Equal(ids, []string{"claude-acp", "goose"}) {
		t.Fatalf("detected = %v", ids)
	}
}

func TestLaunchPrefersAnInstalledBinary(t *testing.T) {
	agent := binaryAgent("kilo", map[string]any{"archive": "https://example.invalid/k.tar.gz", "cmd": "./kilo", "args": []string{"acp"}})
	agent["distribution"].(map[string]any)["npx"] = map[string]any{"package": "@kilocode/cli@7", "args": []string{"acp"}}

	launch, err := fakeRegistry(t, []string{"kilo", "npx"}, agent).Launch(t.Context(), "kilo")
	if err != nil || launch.Command != "/usr/local/bin/kilo" || !slices.Equal(launch.Args, []string{"acp"}) {
		t.Fatalf("launch = %+v, %v", launch, err)
	}
	launch, err = fakeRegistry(t, []string{"npx"}, agent).Launch(t.Context(), "kilo")
	if err != nil || launch.Command != "/usr/local/bin/npx" || !slices.Equal(launch.Args, []string{"--yes", "@kilocode/cli@7", "acp"}) {
		t.Fatalf("npx launch = %+v, %v", launch, err)
	}
}

func TestLaunchPassesRegistryEnv(t *testing.T) {
	launch, err := fakeRegistry(t, []string{"npx"}, npxAgent("auggie", "@augmentcode/auggie@0.37.0")).Launch(t.Context(), "auggie")
	if err != nil || !slices.Equal(launch.Env, []string{"NO_UPDATE=1"}) {
		t.Fatalf("launch = %+v, %v", launch, err)
	}
	if _, err := fakeRegistry(t, nil, npxAgent("auggie", "@augmentcode/auggie@0.37.0")).Launch(t.Context(), "auggie"); err == nil {
		t.Fatal("launched an npx agent without npx")
	}
}

func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	zipped := gzip.NewWriter(&out)
	archive := tar.NewWriter(zipped)
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for name := range files {
			if !yield(name) {
				return
			}
		}
	}) {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(files[name])), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func serveArchive(t *testing.T, data []byte) (string, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	t.Cleanup(server.Close)
	sum := sha256.Sum256(data)
	return server.URL + "/agent.tar.gz", hex.EncodeToString(sum[:])
}

func TestLaunchInstallsAVerifiedArchive(t *testing.T) {
	url, sum := serveArchive(t, tarball(t, map[string]string{"bin/agent": "#!/bin/sh\n"}))
	r := fakeRegistry(t, nil, binaryAgent("packed", map[string]any{"archive": url, "cmd": "./bin/agent", "args": []string{"acp"}, "sha256": sum}))
	launch, err := r.Launch(t.Context(), "packed")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(launch.Command)
	if err != nil || info.Mode().Perm()&0o100 == 0 || !strings.HasPrefix(launch.Command, filepath.Join(r.dir, "agents", "packed", "1")) {
		t.Fatalf("installed %q: %v %v", launch.Command, info, err)
	}

	wrong := fakeRegistry(t, nil, binaryAgent("packed", map[string]any{"archive": url, "cmd": "./bin/agent", "sha256": strings.Repeat("0", 64)}))
	if _, err := wrong.Launch(t.Context(), "packed"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum mismatch launched: %v", err)
	}
	unsigned := fakeRegistry(t, nil, binaryAgent("packed", map[string]any{"archive": url, "cmd": "./bin/agent"}))
	if _, err := unsigned.Launch(t.Context(), "packed"); err == nil {
		t.Fatal("installed an archive without a checksum")
	}
}

func TestInstallRefusesPathsOutsideItsFolder(t *testing.T) {
	url, sum := serveArchive(t, tarball(t, map[string]string{"../escape": "x", "agent": "#!/bin/sh\n"}))
	r := fakeRegistry(t, nil, binaryAgent("escape", map[string]any{"archive": url, "cmd": "./agent", "sha256": sum}))
	if _, err := r.Launch(t.Context(), "escape"); err == nil {
		t.Fatal("unpacked an entry outside the agent's folder")
	}
}
