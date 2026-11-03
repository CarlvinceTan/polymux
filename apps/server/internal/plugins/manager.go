package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

// DefaultIndex is the only thing the server ships about plugins. polymux.com serves the
// official plugins repository's index.json.
const DefaultIndex = "https://polymux.com/plugins/index.json"

const (
	indexTimeout  = 15 * time.Second
	indexOverride = "index-url"
)

var ErrUnknown = errors.New("That plugin isn't installed.")

type Config struct {
	Dir   string
	Index string
	// Publish is what transport plugins may expose.
	Publish Publish
	Store   *store.Store
	Log     *slog.Logger
}

// record is a plugin's saved state; the manifest stays empty until a package is verified.
type record struct {
	Manifest Manifest `json:"manifest"`
	State    string   `json:"state"`
	Issue    string   `json:"issue,omitempty"`
	Source   string   `json:"source"`
	Title    string   `json:"title"`
	Symbol   string   `json:"symbol"`
	Kind     string   `json:"kind"`
}

type Manager struct {
	config Config
	ctx    context.Context
	cancel context.CancelFunc
	client *http.Client
	index  *http.Client

	mu       sync.Mutex
	services map[string]*service
	work     sync.WaitGroup
	// Serialises saves, so a finished install can't overwrite a newer record.
	saves sync.Mutex
}

func New(ctx context.Context, config Config) (*Manager, error) {
	if config.Index == "" {
		config.Index = DefaultIndex
	}
	if err := os.MkdirAll(config.Dir, 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{config: config, ctx: ctx, cancel: cancel, services: map[string]*service{},
		client: &http.Client{},
		index: &http.Client{Timeout: indexTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}}
	_ = os.RemoveAll(m.staging())
	records, err := config.Store.Plugins(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	for _, saved := range records {
		var value record
		if err := json.Unmarshal(saved.Data, &value); err != nil {
			config.Log.Warn("skip unreadable plugin", "plugin", saved.Name, "error", err)
			continue
		}
		switch value.State {
		case contract.PluginStateInstalling:
			value.State, value.Issue = contract.PluginStateFailed, "The install was interrupted."
			if err := m.save(ctx, saved.Name, value); err != nil {
				cancel()
				return nil, err
			}
		case contract.PluginStateReady:
			m.launch(value.Manifest)
		}
	}
	return m, nil
}

func (m *Manager) staging() string { return filepath.Join(m.config.Dir, ".staging") }

func (m *Manager) home(name string) string { return filepath.Join(m.config.Dir, name) }

func (m *Manager) save(ctx context.Context, name string, value record) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return m.config.Store.SavePlugin(ctx, name, data)
}

func (m *Manager) List(ctx context.Context) (contract.PluginList, error) {
	records, err := m.config.Store.Plugins(ctx)
	if err != nil {
		return contract.PluginList{}, err
	}
	installed := make([]contract.Plugin, 0, len(records))
	for _, saved := range records {
		plugin, err := present(saved)
		if err != nil {
			continue
		}
		installed = append(installed, plugin)
	}
	layout, err := m.layout(ctx, installed)
	if err != nil {
		return contract.PluginList{}, err
	}
	return contract.PluginList{Installed: installed, Layout: layout}, nil
}

func present(saved store.PluginRecord) (contract.Plugin, error) {
	var value record
	if err := json.Unmarshal(saved.Data, &value); err != nil {
		return contract.Plugin{}, err
	}
	plugin := contract.Plugin{Name: saved.Name, Title: value.Title, Symbol: value.Symbol,
		SelectedSymbol: value.Manifest.SelectedSymbol, Kind: value.Kind,
		Description: value.Manifest.Description, Version: value.Manifest.Version, Surface: value.Manifest.Surface,
		State: value.State, Source: value.Source, Chats: saved.Chats}
	if value.Issue != "" {
		plugin.Issue = &value.Issue
	}
	if value.Manifest.Surface == SurfaceWeb {
		page, hide := filepath.Base(value.Manifest.Page), value.Manifest.HideClose
		plugin.Page, plugin.HideClose = &page, &hide
	}
	return plugin, nil
}

func (m *Manager) layout(ctx context.Context, installed []contract.Plugin) (contract.PluginLayout, error) {
	saved := contract.PluginLayout{}
	data, err := m.config.Store.PluginLayout(ctx)
	if err != nil {
		return saved, err
	}
	if data != nil {
		if err := json.Unmarshal(data, &saved); err != nil {
			m.config.Log.Warn("reset unreadable plugin layout", "error", err)
			saved = contract.PluginLayout{}
		}
	}
	return Normalise(saved, installed), nil
}

// Normalise keeps saved positions, drops what's gone, and appends what's new.
// Chats always stays in the switcher, wherever it was put. A both plugin's global view
// sits beside the chats, so only its chat button is arranged here.
func Normalise(layout contract.PluginLayout, installed []contract.Plugin) contract.PluginLayout {
	var global, scoped []string
	for _, plugin := range installed {
		if plugin.Surface == "" {
			continue
		}
		if plugin.Kind == KindGlobal {
			global = append(global, plugin.Name)
		} else {
			scoped = append(scoped, plugin.Name)
		}
	}
	out := contract.PluginLayout{Switcher: []string{}, Buttons: [][]string{}}
	for _, name := range layout.Switcher {
		if (name == Chats || slices.Contains(global, name)) && !slices.Contains(out.Switcher, name) {
			out.Switcher = append(out.Switcher, name)
		}
	}
	if !slices.Contains(out.Switcher, Chats) {
		out.Switcher = append([]string{Chats}, out.Switcher...)
	}
	for _, name := range global {
		if !slices.Contains(out.Switcher, name) {
			out.Switcher = append(out.Switcher, name)
		}
	}
	placed := map[string]bool{}
	for _, group := range layout.Buttons {
		kept := []string{}
		for _, name := range group {
			if slices.Contains(scoped, name) && !placed[name] {
				placed[name] = true
				kept = append(kept, name)
			}
		}
		if len(kept) > 0 {
			out.Buttons = append(out.Buttons, kept)
		}
	}
	for _, name := range scoped {
		if !placed[name] {
			out.Buttons = append(out.Buttons, []string{name})
		}
	}
	return out
}

func (m *Manager) SetLayout(ctx context.Context, layout contract.PluginLayout) (contract.PluginLayout, error) {
	list, err := m.List(ctx)
	if err != nil {
		return layout, err
	}
	normal := Normalise(layout, list.Installed)
	data, err := json.Marshal(normal)
	if err != nil {
		return layout, err
	}
	return normal, m.config.Store.SavePluginLayout(ctx, data)
}

// readIndex falls back to the last good copy, so the marketplace still lists offline.
func (m *Manager) readIndex(ctx context.Context) (Index, error) {
	copyPath := filepath.Join(m.config.Dir, "index.json")
	fresh, failure := m.fetchIndex(ctx)
	if failure == nil {
		if data, err := json.Marshal(fresh); err == nil {
			_ = os.WriteFile(copyPath, data, 0o600)
		}
		return fresh, nil
	}
	m.config.Log.Warn("read plugin index", "error", failure)
	data, err := os.ReadFile(copyPath)
	if err != nil {
		return Index{}, failure
	}
	return parseIndex(data)
}

// address lets a server keep its own index by naming it in the plugins folder.
func (m *Manager) address() string {
	if m.config.Index != DefaultIndex {
		return m.config.Index
	}
	if data, err := os.ReadFile(filepath.Join(m.config.Dir, indexOverride)); err == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}
	return m.config.Index
}

func (m *Manager) fetchIndex(ctx context.Context) (Index, error) {
	body, err := m.fetch(ctx, m.address(), m.index)
	if err != nil {
		return Index{}, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, indexLimit+1))
	if err != nil {
		return Index{}, err
	}
	if len(data) > indexLimit {
		return Index{}, errors.New("plugin index is too large")
	}
	return parseIndex(data)
}

func (m *Manager) Market(ctx context.Context) ([]contract.MarketPlugin, error) {
	index, err := m.readIndex(ctx)
	if err != nil {
		return nil, err
	}
	records, err := m.config.Store.Plugins(ctx)
	if err != nil {
		return nil, err
	}
	installed := map[string]string{}
	for _, saved := range records {
		if plugin, err := present(saved); err == nil && plugin.State == contract.PluginStateReady {
			installed[saved.Name] = plugin.Version
		}
	}
	out := make([]contract.MarketPlugin, 0, len(index.Plugins))
	for _, entry := range index.Plugins {
		item := contract.MarketPlugin{Name: entry.Name, Title: entry.Title, Description: entry.Description, Symbol: entry.Symbol,
			Author: entry.Author, Homepage: entry.Homepage, Kind: entry.Kind, Surface: entry.Surface}
		if release, ok := entry.Recommended(Target()); ok {
			item.Version, item.Compatible, item.Source = release.Version, true, sourceLabel(release, Target())
		} else {
			item.Version = entry.Versions[0].Version
		}
		if version, ok := installed[entry.Name]; ok {
			item.Installed = &version
		}
		out = append(out, item)
	}
	return out, nil
}

func sourceLabel(release Release, target string) string {
	if release.Source.Repository != "" {
		label := release.Source.Repository
		if len(release.Source.Commit) >= 7 {
			label += "@" + release.Source.Commit[:7]
		}
		return label
	}
	if parsed, err := url.Parse(release.Source.Artifacts[target].URL); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return release.Source.Artifacts[target].URL
}

// Install returns at once with the plugin in its installing state.
func (m *Manager) Install(ctx context.Context, name, version string) (contract.Plugin, error) {
	index, err := m.readIndex(ctx)
	if err != nil {
		return contract.Plugin{}, err
	}
	position := slices.IndexFunc(index.Plugins, func(entry Entry) bool { return entry.Name == name })
	if position < 0 {
		return contract.Plugin{}, errNotListed
	}
	entry := index.Plugins[position]
	release, ok := entry.Recommended(Target())
	if version != "" {
		number := slices.IndexFunc(entry.Versions, func(item Release) bool { return item.Version == version })
		if number < 0 || !entry.Versions[number].Compatible(Target()) {
			return contract.Plugin{}, errors.New("That version doesn't work on this server.")
		}
		release, ok = entry.Versions[number], true
	}
	if !ok {
		return contract.Plugin{}, errors.New("This plugin doesn't work on this server.")
	}
	value := record{State: contract.PluginStateInstalling, Source: sourceLabel(release, Target()),
		Title: entry.Title, Symbol: entry.Symbol, Kind: entry.Kind,
		Manifest: Manifest{Name: name, Description: entry.Description, Version: release.Version, Surface: entry.Surface,
			Provides: m.provides(ctx, name)}}
	m.mu.Lock()
	_, busy := m.services[name]
	if busy {
		m.mu.Unlock()
		m.stopService(name)
		m.mu.Lock()
	}
	m.work.Add(1)
	m.mu.Unlock()
	if err := m.save(ctx, name, value); err != nil {
		m.work.Done()
		return contract.Plugin{}, err
	}
	// The goroutine finishes its own copy, since the reply below reads value as it was.
	go func(value record) {
		defer m.work.Done()
		manifest, err := m.place(entry, release)
		if err != nil {
			m.config.Log.Warn("install plugin", "plugin", name, "error", err)
			value.State, value.Issue = contract.PluginStateFailed, "The install didn't finish."
			var fixed publicError
			if errors.As(err, &fixed) {
				value.Issue = fixed.Error()
			}
		} else {
			value.State, value.Manifest, value.Kind = contract.PluginStateReady, manifest, manifest.Kind
			value.Title, value.Symbol = manifest.Title, manifest.Symbol
		}
		m.saves.Lock()
		defer m.saves.Unlock()
		if err := m.save(m.ctx, name, value); err != nil {
			m.config.Log.Error("save plugin", "plugin", name, "error", err)
		}
		if value.State == contract.PluginStateReady {
			m.launch(manifest)
		}
	}(value)
	return contract.Plugin{Name: name, Title: value.Title, Description: entry.Description, Symbol: value.Symbol,
		Version: release.Version, Kind: value.Kind, Surface: entry.Surface, State: value.State, Source: value.Source, Chats: []string{}}, nil
}

// provides keeps a plugin's capabilities through a reinstall, so a gate binds its chats until the new package runs.
func (m *Manager) provides(ctx context.Context, name string) []string {
	records, err := m.config.Store.Plugins(ctx)
	if err != nil {
		return nil
	}
	for _, saved := range records {
		var value record
		if saved.Name == name && json.Unmarshal(saved.Data, &value) == nil {
			return value.Manifest.Provides
		}
	}
	return nil
}

type publicError struct{ message string }

func (e publicError) Error() string { return e.message }

// place verifies a package and swaps it in. A package that disagrees with its index row is refused.
func (m *Manager) place(entry Entry, release Release) (Manifest, error) {
	archive, err := m.download(m.ctx, release.Source.Artifacts[Target()])
	if err != nil {
		return Manifest{}, fmt.Errorf("download: %w", err)
	}
	defer os.Remove(archive)
	staged, err := os.MkdirTemp(m.staging(), "unpack-*")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(staged)
	if err := unpack(archive, staged); err != nil {
		return Manifest{}, fmt.Errorf("unpack: %w", err)
	}
	manifest, err := readManifest(staged)
	if err != nil {
		return Manifest{}, publicError{err.Error()}
	}
	if manifest.Name != entry.Name || manifest.Version != release.Version {
		return Manifest{}, publicError{"The package doesn't match its listing."}
	}
	if manifest.Service != "" {
		if info, err := os.Stat(filepath.Join(staged, manifest.Service)); err != nil || info.Mode()&0o100 == 0 {
			return Manifest{}, publicError{"The package's service can't run."}
		}
	}
	if err := m.swap(entry.Name, staged); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// swap makes a staged folder the plugin's code, and keeps the old code if that fails.
func (m *Manager) swap(name, staged string) error {
	home := m.home(name)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	code := filepath.Join(home, "package")
	retired := code + ".old"
	_ = os.RemoveAll(retired)
	if err := os.Rename(code, retired); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staged, code); err != nil {
		_ = os.Rename(retired, code)
		return err
	}
	return os.RemoveAll(retired)
}

func (m *Manager) launch(manifest Manifest) {
	if manifest.Service == "" {
		return
	}
	name := manifest.Name
	root := filepath.Join(m.home(name), "package")
	unit := &service{name: name, binary: filepath.Join(root, manifest.Service), root: root,
		data: filepath.Join(m.home(name), "data"), log: m.config.Log, env: m.serviceEnv(manifest),
		running: func() { m.setHealth(name, contract.PluginStateReady, "") },
		failed:  func(issue string) { m.setHealth(name, contract.PluginStateFailed, issue) }}
	m.mu.Lock()
	m.services[name] = unit
	m.mu.Unlock()
	unit.start(m.ctx)
}

func (m *Manager) serviceEnv(manifest Manifest) []string {
	env := []string{"POLYMUX_PLUGIN_SETTINGS=" + m.settingsPath(manifest.Name)}
	if slices.Contains(manifest.Provides, CapabilityTransport) {
		env = append(env, "POLYMUX_PUBLISH_API="+m.config.Publish.API, "POLYMUX_PUBLISH_DEVICE="+m.config.Publish.Device)
	}
	return env
}

func (m *Manager) setHealth(name, state, issue string) {
	m.saves.Lock()
	defer m.saves.Unlock()
	records, err := m.config.Store.Plugins(m.ctx)
	if err != nil {
		return
	}
	for _, saved := range records {
		var value record
		if saved.Name != name || json.Unmarshal(saved.Data, &value) != nil || value.State == state && value.Issue == issue {
			continue
		}
		value.State, value.Issue = state, issue
		if err := m.save(m.ctx, name, value); err != nil {
			m.config.Log.Error("save plugin health", "plugin", name, "error", err)
		}
	}
}

func (m *Manager) stopService(name string) {
	m.mu.Lock()
	unit := m.services[name]
	delete(m.services, name)
	m.mu.Unlock()
	if unit != nil {
		unit.stop()
	}
}

func (m *Manager) Remove(ctx context.Context, name string) error {
	if !m.known(ctx, name) {
		return ErrUnknown
	}
	m.stopService(name)
	if err := m.config.Store.RemovePlugin(ctx, name); err != nil {
		return err
	}
	return os.RemoveAll(m.home(name))
}

func (m *Manager) known(ctx context.Context, name string) bool {
	records, err := m.config.Store.Plugins(ctx)
	return err == nil && slices.ContainsFunc(records, func(saved store.PluginRecord) bool { return saved.Name == name })
}

func (m *Manager) SetChats(ctx context.Context, name string, chats []string) (contract.Plugin, error) {
	list, err := m.List(ctx)
	if err != nil {
		return contract.Plugin{}, err
	}
	position := slices.IndexFunc(list.Installed, func(plugin contract.Plugin) bool { return plugin.Name == name })
	if position < 0 {
		return contract.Plugin{}, ErrUnknown
	}
	plugin := list.Installed[position]
	if plugin.Kind == KindGlobal {
		return contract.Plugin{}, errors.New("Global plugins apply everywhere.")
	}
	if err := m.config.Store.SetPluginChats(ctx, name, chats); err != nil {
		return contract.Plugin{}, err
	}
	plugin.Chats = chats
	return plugin, nil
}

// Forward sends a request to a plugin's service.
func (m *Manager) Forward(w http.ResponseWriter, r *http.Request, name, path string) bool {
	m.mu.Lock()
	unit := m.services[name]
	m.mu.Unlock()
	return unit != nil && unit.forward(w, r, path)
}

// SkillFiles lists the skill files that apply to a chat: every global plugin's, and those of Scoped plugins active in it.
func (m *Manager) SkillFiles(ctx context.Context, chat string) []string {
	records, err := m.config.Store.Plugins(ctx)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, saved := range records {
		var value record
		if json.Unmarshal(saved.Data, &value) != nil || value.State != contract.PluginStateReady {
			continue
		}
		if value.Kind == KindScoped && !slices.Contains(saved.Chats, chat) {
			continue
		}
		for _, skill := range value.Manifest.Skills {
			if strings.HasSuffix(skill, ".md") {
				out = append(out, filepath.Join(m.home(saved.Name), "package", skill))
			}
		}
	}
	return out
}

func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	units := make([]*service, 0, len(m.services))
	for _, unit := range m.services {
		units = append(units, unit)
	}
	m.services = map[string]*service{}
	m.mu.Unlock()
	for _, unit := range units {
		unit.stop()
	}
	m.work.Wait()
}
