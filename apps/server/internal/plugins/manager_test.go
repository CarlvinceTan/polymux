package plugins

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

// TestMain doubles as a plugin service when a package's script re-enters this binary.
func TestMain(m *testing.M) {
	if port := os.Getenv("POLYMUX_PLUGIN_PORT"); port != "" {
		token := os.Getenv("POLYMUX_PLUGIN_TOKEN")
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Cookie") != "" {
				w.WriteHeader(401)
				return
			}
			if r.URL.Path == "/gate" {
				var call Call
				_ = json.NewDecoder(r.Body).Decode(&call)
				switch call.Tool {
				case "denied":
					fmt.Fprint(w, `{"allow":false,"reason":"Denied in `+call.Chat+`"}`)
				case "silent":
					fmt.Fprint(w, `{}`)
				default:
					fmt.Fprint(w, `{"allow":true}`)
				}
				return
			}
			if strings.HasPrefix(r.URL.Path, "/tools/") {
				var call ToolCall
				_ = json.NewDecoder(r.Body).Decode(&call)
				_ = json.NewEncoder(w).Encode(ToolResult{Text: r.URL.Path + " " + call.Chat + " " + call.Agent + " " + string(call.Args)})
				return
			}
			if r.URL.Path == "/transport" {
				port := "8443"
				var saved map[string]string
				if data, err := os.ReadFile(os.Getenv("POLYMUX_PLUGIN_SETTINGS")); err == nil && json.Unmarshal(data, &saved) == nil && saved["api-port"] != "" {
					port = saved["api-port"]
				}
				_, device, _ := strings.Cut(os.Getenv("POLYMUX_PUBLISH_DEVICE"), ":")
				fmt.Fprintf(w, `{"state":"ready","api":"https://x.example:%s","device":"https://x.example:%s"}`, port, device)
				return
			}
			fmt.Fprint(w, r.URL.Path)
		})
		_ = http.ListenAndServe("127.0.0.1:"+port, handler)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

type file struct {
	name string
	body string
	mode int64
	kind byte
}

func archive(t *testing.T, files []file) []byte {
	t.Helper()
	var output bytes.Buffer
	stream := gzip.NewWriter(&output)
	writer := tar.NewWriter(stream)
	for _, item := range files {
		kind := item.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := item.mode
		if mode == 0 {
			mode = 0o644
		}
		if err := writer.WriteHeader(&tar.Header{Name: item.name, Mode: mode, Size: int64(len(item.body)), Typeflag: kind, Linkname: "/etc/hosts"}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(item.body)); err != nil {
			t.Fatal(err)
		}
	}
	writer.Close()
	stream.Close()
	return output.Bytes()
}

func manifest(t *testing.T, value Manifest) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type market struct {
	index    Index
	packages map[string][]byte
	server   *httptest.Server
}

func newMarket(t *testing.T) *market {
	t.Helper()
	value := &market{index: Index{Format: 1, Title: "Test"}, packages: map[string][]byte{}}
	value.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			_ = json.NewEncoder(w).Encode(value.index)
			return
		}
		body, ok := value.packages[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(value.server.Close)
	return value
}

func (v *market) list(name, kind, version string, body []byte, sum string) {
	if sum == "" {
		digest := sha256.Sum256(body)
		sum = hex.EncodeToString(digest[:])
	}
	path := "/" + name + "-" + version + ".tgz"
	v.packages[path] = body
	v.index.Plugins = append(v.index.Plugins, Entry{Name: name, Title: name, Kind: kind, Surface: name, Symbol: "star",
		Versions: []Release{{Version: version, Source: Source{Artifacts: map[string]Artifact{
			Target(): {URL: v.server.URL + path, SHA256: sum}}}}}})
}

func manager(t *testing.T, value *market) (*Manager, *store.Store) {
	t.Helper()
	data, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { data.Close() })
	m, err := New(context.Background(), Config{Dir: filepath.Join(t.TempDir(), "plugins"), Index: value.server.URL + "/index.json",
		Publish: Publish{API: "127.0.0.1:7341", Device: "127.0.0.1:7339"}, Store: data, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, data
}

func settled(t *testing.T, m *Manager, name string) contract.Plugin {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		list, err := m.List(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, plugin := range list.Installed {
			if plugin.Name == name && plugin.State != contract.PluginStateInstalling {
				return plugin
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never settled", name)
	return contract.Plugin{}
}

func TestInstallRunsServiceAndForwards(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	value := newMarket(t)
	value.list("demo", KindGlobal, "1.0.0", archive(t, []file{
		{name: "plugin.json", body: manifest(t, Manifest{Name: "demo", Title: "Demo", Version: "1.0.0", Kind: KindGlobal, Surface: "demo", Symbol: "star", SelectedSymbol: "star.fill", Service: "bin/run"})},
		{name: "bin/run", body: "#!/bin/sh\nexec " + binary + "\n", mode: 0o755},
	}), "")
	m, _ := manager(t, value)
	if _, err := m.Install(t.Context(), "demo", ""); err != nil {
		t.Fatal(err)
	}
	if plugin := settled(t, m, "demo"); plugin.State != contract.PluginStateReady {
		t.Fatalf("state %s issue %v", plugin.State, plugin.Issue)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("GET", "/plugins/demo/api/x/y", nil)
		request.Header.Set("Cookie", "secret=1")
		if m.Forward(recorder, request, "demo", "x/y") {
			if recorder.Code != 200 || recorder.Body.String() != "/x/y" {
				t.Fatalf("forward %d %q", recorder.Code, recorder.Body.String())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("service never came up")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := m.Remove(t.Context(), "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.home("demo")); !os.IsNotExist(err) {
		t.Fatalf("plugin files survived removal: %v", err)
	}
	if m.Forward(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), "demo", "x") {
		t.Fatal("removed plugin still answers")
	}
}

func TestPackagesAreRefused(t *testing.T) {
	good := file{name: "plugin.json", body: `{"name":"bad","title":"Bad","version":"1.0.0","kind":"global"}`}
	cases := map[string]struct {
		files []file
		sum   string
	}{
		"checksum":        {files: []file{good}, sum: string(bytes.Repeat([]byte("0"), 64))},
		"traversal":       {files: []file{good, {name: "../escape", body: "x"}}},
		"symlink":         {files: []file{good, {name: "link", kind: tar.TypeSymlink}}},
		"no manifest":     {files: []file{{name: "readme", body: "x"}}},
		"other name":      {files: []file{{name: "plugin.json", body: `{"name":"other","title":"Bad","version":"1.0.0","kind":"global"}`}}},
		"newer api":       {files: []file{{name: "plugin.json", body: `{"name":"bad","version":"1.0.0","kind":"global","engines":{"plugins":99}}`}}},
		"no service":      {files: []file{{name: "plugin.json", body: `{"name":"bad","version":"1.0.0","kind":"global","service":"bin/run"}`}}},
		"outside":         {files: []file{{name: "plugin.json", body: `{"name":"bad","version":"1.0.0","kind":"global","skills":["../../x.md"]}`}}},
		"one icon":        {files: []file{{name: "plugin.json", body: `{"name":"bad","version":"1.0.0","kind":"global","surface":"bad","symbol":"star"}`}}},
		"same icon":       {files: []file{{name: "plugin.json", body: `{"name":"bad","version":"1.0.0","kind":"global","surface":"bad","symbol":"star","selectedSymbol":"star"}`}}},
		"global gate":     {files: []file{{name: "plugin.json", body: `{"name":"bad","version":"1.0.0","kind":"global","service":"bin/run","provides":["gate"]}`}, {name: "bin/run", body: "x", mode: 0o755}}},
		"no gate service": {files: []file{{name: "plugin.json", body: `{"name":"bad","version":"1.0.0","kind":"scoped","provides":["gate"]}`}}},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			value := newMarket(t)
			value.list("bad", KindGlobal, "1.0.0", archive(t, input.files), input.sum)
			m, _ := manager(t, value)
			if _, err := m.Install(t.Context(), "bad", ""); err != nil {
				t.Fatal(err)
			}
			if plugin := settled(t, m, "bad"); plugin.State != contract.PluginStateFailed {
				t.Fatalf("state %s", plugin.State)
			}
			if _, err := os.Stat(filepath.Join(m.home("bad"), "package")); !os.IsNotExist(err) {
				t.Fatal("refused package was installed")
			}
		})
	}
}

// A gate binds only its chats, and nothing short of an allow lets a call through.
func TestGateDecidesItsChats(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body := archive(t, []file{
		{name: "plugin.json", body: manifest(t, Manifest{Name: "guard", Title: "Guard", Version: "1.0.0", Kind: KindScoped, Service: "bin/run", Provides: []string{CapabilityGate}})},
		{name: "bin/run", body: "#!/bin/sh\nexec " + binary + "\n", mode: 0o755},
	})
	value := newMarket(t)
	value.list("guard", KindScoped, "1.0.0", body, "")
	m, data := manager(t, value)
	if _, err := m.Install(t.Context(), "guard", ""); err != nil {
		t.Fatal(err)
	}
	if plugin := settled(t, m, "guard"); plugin.State != contract.PluginStateReady {
		t.Fatalf("state %s issue %v", plugin.State, plugin.Issue)
	}
	if _, err := data.DB.ExecContext(t.Context(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetChats(t.Context(), "guard", []string{"bound"}); err != nil {
		t.Fatal(err)
	}
	check := func(tool string, chats ...string) Decision {
		return m.Check(t.Context(), chats, Call{Tool: tool, Args: json.RawMessage(`{}`)})
	}
	waitFor := func(want bool) {
		for end := time.Now().Add(10 * time.Second); check("read", "bound").Allow != want; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("gate never reached allow=%v", want)
			}
		}
	}
	waitFor(true)
	if m.Gated(t.Context(), []string{"free"}) || !m.Gated(t.Context(), []string{"free", "bound"}) {
		t.Fatal("gate bound the wrong chats")
	}
	if decision := check("denied", "free"); !decision.Allow {
		t.Fatalf("unbound chat was gated: %+v", decision)
	}
	if decision := check("denied", "free", "bound"); decision.Allow || decision.Reason != "Denied in bound" {
		t.Fatalf("an ancestor chat's rules should bind: %+v", decision)
	}
	if decision := check("silent", "bound"); decision.Allow || decision.Reason != "Guard blocked this call." {
		t.Fatalf("an empty answer allowed the call: %+v", decision)
	}
	m.stopService("guard")
	if decision := check("read", "bound"); decision.Allow || decision.Reason != "Guard isn't running, so the call was blocked." {
		t.Fatalf("a stopped gate let the call through: %+v", decision)
	}
	// A reinstall that fails leaves no service, and the chat stays bound.
	value.index.Plugins = nil
	value.list("guard", KindScoped, "1.0.1", body, strings.Repeat("0", 64))
	if _, err := m.Install(t.Context(), "guard", ""); err != nil {
		t.Fatal(err)
	}
	if plugin := settled(t, m, "guard"); plugin.State != contract.PluginStateFailed {
		t.Fatalf("state %s", plugin.State)
	}
	if check("read", "bound").Allow {
		t.Fatal("a failed reinstall lifted the gate")
	}
}

// The package decides what a plugin is, so an index row can't relabel it.
func TestManifestOverridesIndex(t *testing.T) {
	value := newMarket(t)
	value.list("demo", KindGlobal, "1.0.0", archive(t, []file{
		{name: "plugin.json", body: manifest(t, Manifest{Name: "demo", Title: "Actual", Version: "1.0.0", Kind: KindScoped, Symbol: "bolt"})},
	}), "")
	m, _ := manager(t, value)
	if _, err := m.Install(t.Context(), "demo", ""); err != nil {
		t.Fatal(err)
	}
	plugin := settled(t, m, "demo")
	if plugin.Kind != KindScoped || plugin.Title != "Actual" || plugin.Symbol != "bolt" {
		t.Fatalf("%+v", plugin)
	}
}

func TestRecommendedPrefersStable(t *testing.T) {
	artifacts := map[string]Artifact{"x": {URL: "u", SHA256: "s"}}
	entry := Entry{Versions: []Release{
		{Version: "2.0.0-beta.1", Source: Source{Artifacts: artifacts}},
		{Version: "1.2.0", Source: Source{Artifacts: artifacts}},
		{Version: "1.10.0", Source: Source{Artifacts: artifacts}},
		{Version: "9.0.0", Engines: Engines{Plugins: APIVersion + 1}, Source: Source{Artifacts: artifacts}},
		{Version: "8.0.0"},
	}}
	if release, ok := entry.Recommended("x"); !ok || release.Version != "1.10.0" {
		t.Fatalf("%v %v", release, ok)
	}
	entry.Versions = entry.Versions[:1]
	if release, ok := entry.Recommended("x"); !ok || release.Version != "2.0.0-beta.1" {
		t.Fatalf("%v %v", release, ok)
	}
	if _, ok := entry.Recommended("other"); ok {
		t.Fatal("recommended a version with no artifact")
	}
}

// A both plugin has a chat button and a global view, so it stays out of the switcher.
func TestBothLeavesTheSwitcherAlone(t *testing.T) {
	installed := []contract.Plugin{{Name: "hub", Kind: KindGlobal, Surface: "hub"}, {Name: "graph", Kind: KindBoth, Surface: "graph"}}
	out := Normalise(contract.PluginLayout{Switcher: []string{Chats, "graph"}}, installed)
	if !slices.Equal(out.Switcher, []string{Chats, "hub"}) {
		t.Fatalf("switcher %v", out.Switcher)
	}
	if len(out.Buttons) != 1 || !slices.Equal(out.Buttons[0], []string{"graph"}) {
		t.Fatalf("buttons %v", out.Buttons)
	}
}

// A both plugin keeps a chat list like a Scoped one, and its tools reach every chat like a Global one's.
func TestBothKeepsChatsAndOffersTools(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tool := Tool{Name: "report", Description: "Report work", Parameters: map[string]any{"type": "object"}}
	body := archive(t, []file{
		{name: "plugin.json", body: manifest(t, Manifest{Name: "graph", Title: "Graph", Version: "1.0.0", Kind: KindBoth, Surface: "graph", Symbol: "a", SelectedSymbol: "a.fill",
			Service: "bin/run", Provides: []string{CapabilityTools}, Tools: []Tool{tool}})},
		{name: "bin/run", body: "#!/bin/sh\nexec " + binary + "\n", mode: 0o755},
	})
	value := newMarket(t)
	value.list("graph", KindBoth, "1.0.0", body, "")
	m, data := manager(t, value)
	if _, err := m.Install(t.Context(), "graph", ""); err != nil {
		t.Fatal(err)
	}
	if plugin := settled(t, m, "graph"); plugin.State != contract.PluginStateReady || plugin.Kind != KindBoth {
		t.Fatalf("state %s kind %s issue %v", plugin.State, plugin.Kind, plugin.Issue)
	}
	if _, err := data.DB.ExecContext(t.Context(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if plugin, err := m.SetChats(t.Context(), "graph", []string{"chat"}); err != nil || !slices.Equal(plugin.Chats, []string{"chat"}) {
		t.Fatalf("chats %v %v", plugin.Chats, err)
	}
	tools := m.Tools(t.Context(), "another chat")
	if len(tools) != 1 || tools[0].Plugin != "graph" || tools[0].Name != "report" {
		t.Fatalf("tools %v", tools)
	}
	var answer string
	for end := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		answer, err = m.CallTool(t.Context(), "graph", "report", ToolCall{Chat: "c", Agent: "a", Args: json.RawMessage(`{"x":1}`)})
		if err == nil || time.Now().After(end) {
			break
		}
	}
	if err != nil || answer != `/tools/report c a {"x":1}` {
		t.Fatalf("answer %q %v", answer, err)
	}
}

func TestToolsFollowScopedChats(t *testing.T) {
	tool := Tool{Name: "report", Description: "Report work", Parameters: map[string]any{"type": "object"}}
	value := newMarket(t)
	value.list("scoped", KindScoped, "1.0.0", archive(t, []file{{name: "plugin.json", body: manifest(t, Manifest{Name: "scoped", Version: "1.0.0", Kind: KindScoped, Tools: []Tool{tool}, Service: "run", Provides: []string{CapabilityTools}})}, {name: "run", body: "#!/bin/sh\nsleep 60\n", mode: 0o755}}), "")
	m, data := manager(t, value)
	if _, err := m.Install(t.Context(), "scoped", ""); err != nil {
		t.Fatal(err)
	}
	settled(t, m, "scoped")
	if _, err := data.DB.ExecContext(t.Context(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if err := data.SetPluginChats(t.Context(), "scoped", []string{"chat"}); err != nil {
		t.Fatal(err)
	}
	if tools := m.Tools(t.Context(), "elsewhere"); len(tools) != 0 {
		t.Fatalf("inactive chat: %v", tools)
	}
	if tools := m.Tools(t.Context(), "chat"); len(tools) != 1 {
		t.Fatalf("active chat: %v", tools)
	}
}

func TestLayoutKeepsChatsAndFollowsInstalled(t *testing.T) {
	installed := []contract.Plugin{
		{Name: "hub", Kind: KindGlobal, Surface: "hub"},
		{Name: "tasks", Kind: KindGlobal, Surface: "tasks"},
		{Name: "a", Kind: KindScoped, Surface: "a"},
		{Name: "b", Kind: KindScoped, Surface: "b"},
		{Name: "c", Kind: KindScoped, Surface: "c"},
		{Name: "quiet", Kind: KindScoped},
	}
	out := Normalise(contract.PluginLayout{
		Switcher: []string{"hub", "gone", "hub"},
		Buttons:  [][]string{{"b", "gone", "a"}, {"gone"}, {"a"}},
	}, installed)
	if !slices.Equal(out.Switcher, []string{Chats, "hub", "tasks"}) {
		t.Fatalf("switcher %v", out.Switcher)
	}
	if len(out.Buttons) != 2 || !slices.Equal(out.Buttons[0], []string{"b", "a"}) || !slices.Equal(out.Buttons[1], []string{"c"}) {
		t.Fatalf("buttons %v", out.Buttons)
	}
	moved := Normalise(contract.PluginLayout{Switcher: []string{"hub", Chats}}, installed[:1])
	if !slices.Equal(moved.Switcher, []string{"hub", Chats}) {
		t.Fatalf("chats lost its place: %v", moved.Switcher)
	}
	empty := Normalise(contract.PluginLayout{}, nil)
	if !slices.Equal(empty.Switcher, []string{Chats}) || len(empty.Buttons) != 0 {
		t.Fatalf("%+v", empty)
	}
}

func TestMarketServesLastCopyOffline(t *testing.T) {
	value := newMarket(t)
	value.list("demo", KindScoped, "1.0.0", archive(t, []file{{name: "plugin.json", body: "{}"}}), "")
	m, _ := manager(t, value)
	if market, err := m.Market(t.Context()); err != nil || len(market) != 1 || !market[0].Compatible {
		t.Fatalf("%v %v", market, err)
	}
	value.server.Close()
	if market, err := m.Market(t.Context()); err != nil || len(market) != 1 {
		t.Fatalf("offline: %v %v", market, err)
	}
}

func TestSkillFilesFollowChats(t *testing.T) {
	value := newMarket(t)
	value.list("scoped", KindScoped, "1.0.0", archive(t, []file{{name: "plugin.json", body: manifest(t, Manifest{Name: "scoped", Version: "1.0.0", Kind: KindScoped, Skills: []string{"s/SKILL.md"}})}}), "")
	value.list("global", KindGlobal, "1.0.0", archive(t, []file{{name: "plugin.json", body: manifest(t, Manifest{Name: "global", Version: "1.0.0", Kind: KindGlobal, Skills: []string{"g/SKILL.md"}})}}), "")
	m, data := manager(t, value)
	for _, name := range []string{"scoped", "global"} {
		if _, err := m.Install(t.Context(), name, ""); err != nil {
			t.Fatal(err)
		}
		settled(t, m, name)
	}
	if _, err := data.DB.ExecContext(t.Context(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if err := data.SetPluginChats(t.Context(), "scoped", []string{"chat"}); err != nil {
		t.Fatal(err)
	}
	if files := m.SkillFiles(t.Context(), "chat"); len(files) != 2 {
		t.Fatalf("active chat: %v", files)
	}
	if files := m.SkillFiles(t.Context(), "other"); len(files) != 1 || filepath.Base(filepath.Dir(files[0])) != "g" {
		t.Fatalf("other chat: %v", files)
	}
}

func TestIndexFileOverridesDefaultOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, indexOverride), []byte("file:///local/index.json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := (&Manager{config: Config{Dir: dir, Index: DefaultIndex}}).address(); got != "file:///local/index.json" {
		t.Fatal(got)
	}
	if got := (&Manager{config: Config{Dir: dir, Index: "https://example.test/index.json"}}).address(); got != "https://example.test/index.json" {
		t.Fatal(got)
	}
}

func TestTransportReportsAndSettingsRestart(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	value := newMarket(t)
	value.list("net", KindGlobal, "1.0.0", archive(t, []file{
		{name: "plugin.json", body: manifest(t, Manifest{Name: "net", Title: "Net", Version: "1.0.0", Kind: KindGlobal, Service: "bin/run",
			Provides: []string{CapabilityTransport}, Settings: []Setting{{Key: "api-port", Title: "Port", Type: "text", Default: "8443"}}})},
		{name: "bin/run", body: "#!/bin/sh\nexec " + binary + "\n", mode: 0o755},
	}), "")
	m, _ := manager(t, value)
	if _, err := m.Install(t.Context(), "net", ""); err != nil {
		t.Fatal(err)
	}
	if plugin := settled(t, m, "net"); plugin.State != contract.PluginStateReady {
		t.Fatalf("state %s issue %v", plugin.State, plugin.Issue)
	}
	waitAPI := func(want string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			if list := m.Transports(t.Context()); len(list) == 1 && list[0].API == want {
				if list[0].Device != "https://x.example:7339" || list[0].Title != "Net" || list[0].Message != "" {
					t.Fatalf("transport %+v", list[0])
				}
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("transport never reported %s: %+v", want, m.Transports(t.Context()))
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitAPI("https://x.example:8443")
	values, err := m.SetSettings(t.Context(), "net", map[string]any{"api-port": "9443"})
	if err != nil || values[0].Value != "9443" {
		t.Fatalf("set settings: %v %+v", err, values)
	}
	waitAPI("https://x.example:9443")
	if _, err := m.SetSettings(t.Context(), "net", map[string]any{"nope": "1"}); err == nil {
		t.Fatal("undeclared setting was saved")
	}
	if _, err := m.SetSettings(t.Context(), "net", map[string]any{"api-port": true}); err == nil {
		t.Fatal("wrong setting type was saved")
	}
}

func TestCapabilitiesAreChecked(t *testing.T) {
	good := Setting{Key: "port", Title: "Port", Type: "text"}
	for name, value := range map[string]Manifest{
		"unknown":         {Service: "run", Provides: []string{"teleport"}},
		"no service":      {Provides: []string{CapabilityTransport}},
		"duplicate":       {Settings: []Setting{good, good}},
		"untitled":        {Settings: []Setting{{Key: "port", Type: "text"}}},
		"unknown type":    {Settings: []Setting{{Key: "port", Title: "Port", Type: "number"}}},
		"mismatched type": {Settings: []Setting{{Key: "port", Title: "Port", Type: "bool", Default: "yes"}}},
		"gate on both":    {Kind: KindBoth, Service: "run", Provides: []string{CapabilityGate}},
		"tools unclaimed": {Tools: []Tool{{Name: "report", Description: "Report", Parameters: map[string]any{}}}},
		"tool name":       {Service: "run", Provides: []string{CapabilityTools}, Tools: []Tool{{Name: "Bad-Name", Description: "Report", Parameters: map[string]any{}}}},
		"tool blank":      {Service: "run", Provides: []string{CapabilityTools}, Tools: []Tool{{Name: "report", Parameters: map[string]any{}}}},
		"tool twice": {Service: "run", Provides: []string{CapabilityTools}, Tools: []Tool{
			{Name: "report", Description: "Report", Parameters: map[string]any{}}, {Name: "report", Description: "Again", Parameters: map[string]any{}}}},
	} {
		if err := value.validateCapabilities(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := (Manifest{Service: "run", Provides: []string{CapabilityTransport}, Settings: []Setting{good}}).validateCapabilities(); err != nil {
		t.Fatal(err)
	}
}
