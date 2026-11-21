package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// RegistryURL is the ACP registry: every agent a client can launch, how to start it, and its icon.
const RegistryURL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"

// registryAge is how long a fetched registry is used before it is refreshed.
const registryAge = 24 * time.Hour

// scanAge is how long a scan for installed agents is reused.
const scanAge = 5 * time.Minute

// Agent is one registry entry.
type Agent struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Description  string       `json:"description"`
	Distribution distribution `json:"distribution"`
}

type distribution struct {
	NPX    *packageRun       `json:"npx,omitempty"`
	UVX    *packageRun       `json:"uvx,omitempty"`
	Binary map[string]binary `json:"binary,omitempty"`
}

type packageRun struct {
	Package string            `json:"package"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type binary struct {
	Archive string            `json:"archive"`
	Cmd     string            `json:"cmd"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	SHA256  string            `json:"sha256,omitempty"`
}

// Launch is how to start an agent process.
type Launch struct {
	Command string
	Args    []string
	Env     []string
}

// Registry finds external agents: those in the ACP registry, and which of them this machine already has.
type Registry struct {
	dir    string
	url    string
	client *http.Client
	look   func(string) (string, error)

	mu         sync.Mutex
	agents     []Agent
	fetched    time.Time
	loaded     bool
	refreshing bool
	found      []Agent
	scanned    time.Time
}

// NewRegistry reads the registry from url and keeps it, and downloaded agents, in dir. An empty url stays offline.
func NewRegistry(dir, url string) *Registry {
	return &Registry{dir: dir, url: url, client: &http.Client{Timeout: 30 * time.Second}, look: exec.LookPath}
}

// Agents returns the registry. It reads the cached copy and refreshes it in the background once it is a day old.
func (r *Registry) Agents() []Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadCache()
	if r.url != "" && time.Since(r.fetched) > registryAge && !r.refreshing {
		r.refreshing = true
		go func() {
			ctx, stop := context.WithTimeout(context.Background(), time.Minute)
			defer stop()
			_ = r.refresh(ctx)
		}()
	}
	return r.agents
}

// Detected lists the registry agents installed on this machine, found by their commands on PATH.
func (r *Registry) Detected() []Agent {
	agents := r.Agents()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.found != nil && time.Since(r.scanned) < scanAge {
		return r.found
	}
	found := []Agent{}
	for _, agent := range agents {
		if slices.ContainsFunc(agent.names(), func(name string) bool { _, err := r.look(name); return err == nil }) {
			found = append(found, agent)
		}
	}
	r.found, r.scanned = found, time.Now()
	return found
}

// Find returns a registry agent, fetching the registry first when there is no copy yet.
func (r *Registry) Find(ctx context.Context, id string) (Agent, bool) {
	agents := r.Agents()
	if len(agents) == 0 && r.url != "" {
		if err := r.refresh(ctx); err == nil {
			agents = r.Agents()
		}
	}
	i := slices.IndexFunc(agents, func(a Agent) bool { return a.ID == id })
	if i < 0 {
		return Agent{}, false
	}
	return agents[i], true
}

// Launch resolves how to start a registry agent: its installed command, then npx or uvx, then a verified download.
func (r *Registry) Launch(ctx context.Context, id string) (Launch, error) {
	agent, ok := r.Find(ctx, id)
	if !ok {
		return Launch{}, fmt.Errorf("acp: %q is not in the ACP registry", id)
	}
	b, hasBinary := agent.Distribution.Binary[platform()]
	if hasBinary {
		if path, err := r.look(filepath.Base(b.Cmd)); err == nil {
			return Launch{Command: path, Args: b.Args, Env: environ(b.Env)}, nil
		}
	}
	if p := agent.Distribution.NPX; p != nil {
		if path, err := r.look("npx"); err == nil {
			return Launch{Command: path, Args: append([]string{"--yes", p.Package}, p.Args...), Env: environ(p.Env)}, nil
		}
	}
	if p := agent.Distribution.UVX; p != nil {
		if path, err := r.look("uvx"); err == nil {
			return Launch{Command: path, Args: append([]string{p.Package}, p.Args...), Env: environ(p.Env)}, nil
		}
	}
	if hasBinary {
		path, err := r.install(ctx, agent, b)
		if err != nil {
			return Launch{}, err
		}
		return Launch{Command: path, Args: b.Args, Env: environ(b.Env)}, nil
	}
	return Launch{}, fmt.Errorf("acp: %s needs npx or uvx on the server", agent.Name)
}

// loadCache reads the saved registry once. The caller holds r.mu.
func (r *Registry) loadCache() {
	if r.loaded {
		return
	}
	r.loaded = true
	path := filepath.Join(r.dir, "registry.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	agents, err := parseRegistry(data)
	if err != nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	r.agents, r.fetched = agents, info.ModTime()
}

// refresh downloads the registry and saves it. A failed fetch keeps the cached copy.
func (r *Registry) refresh(ctx context.Context) error {
	defer func() {
		r.mu.Lock()
		r.refreshing = false
		r.mu.Unlock()
	}()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("acp: registry returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return err
	}
	agents, err := parseRegistry(data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(r.dir, "registry-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), filepath.Join(r.dir, "registry.json")); err != nil {
		return err
	}
	r.mu.Lock()
	r.agents, r.fetched, r.loaded, r.found = agents, time.Now(), true, nil
	r.mu.Unlock()
	return nil
}

func parseRegistry(data []byte) ([]Agent, error) {
	var registry struct {
		Agents []Agent `json:"agents"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}
	if len(registry.Agents) == 0 {
		return nil, errors.New("acp: registry lists no agents")
	}
	return registry.Agents, nil
}

// genericNames are package names too common to mean the agent is installed.
var genericNames = []string{"agent", "cli", "code"}

// names are the commands that show an agent is installed: its ID without -acp, its binary, and its package.
func (a Agent) names() []string {
	names := []string{strings.TrimSuffix(a.ID, "-acp")}
	if b, ok := a.Distribution.Binary[platform()]; ok {
		names = append(names, filepath.Base(b.Cmd))
	}
	for _, p := range []*packageRun{a.Distribution.NPX, a.Distribution.UVX} {
		if p != nil {
			names = append(names, packageName(p.Package))
		}
	}
	return slices.DeleteFunc(slices.Compact(names), func(name string) bool {
		return name == "" || slices.Contains(genericNames, name)
	})
}

// packageName drops a package's scope and version: @scope/name@1.0 and name==1.0 become name.
func packageName(spec string) string {
	spec = spec[strings.LastIndex(spec, "/")+1:]
	if i := strings.IndexAny(spec, "@="); i > 0 {
		spec = spec[:i]
	}
	return spec
}

// platform is this machine in the registry's terms, such as darwin-aarch64.
func platform() string {
	arch := map[string]string{"arm64": "aarch64", "amd64": "x86_64"}[runtime.GOARCH]
	if arch == "" {
		arch = runtime.GOARCH
	}
	return runtime.GOOS + "-" + arch
}

func environ(env map[string]string) []string {
	out := []string{}
	for _, key := range slices.Sorted(maps.Keys(env)) {
		out = append(out, key+"="+env[key])
	}
	return out
}
