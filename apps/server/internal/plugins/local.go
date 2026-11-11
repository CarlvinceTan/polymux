package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// LocalSource starts the source of a plugin a bot built here, which no index lists.
const LocalSource = "Made by "

func (m *Manager) pending(name string) string { return filepath.Join(m.config.Dir, ".pending", name) }

func (m *Manager) record(ctx context.Context, name string) (record, bool) {
	records, err := m.config.Store.Plugins(ctx)
	if err != nil {
		return record{}, false
	}
	for _, saved := range records {
		if saved.Name == name {
			var value record
			err := json.Unmarshal(saved.Data, &value)
			return value, err == nil
		}
	}
	return record{}, false
}

// Stage checks a folder a bot built and keeps a copy until someone confirms it.
func (m *Manager) Stage(ctx context.Context, folder string) (Manifest, error) {
	manifest, err := readManifest(folder)
	if err != nil {
		return Manifest{}, err
	}
	if manifest.Version == "" {
		return Manifest{}, errors.New("The package needs a version.")
	}
	if manifest.Surface != "" && manifest.Surface != SurfaceWeb {
		return Manifest{}, errors.New("A local plugin's screen has to be a web page.")
	}
	if slices.ContainsFunc(manifest.Provides, func(item string) bool { return item != CapabilityTools }) {
		return Manifest{}, errors.New("A local plugin can only offer tools.")
	}
	if err := m.replaceable(ctx, manifest.Name); err != nil {
		return Manifest{}, err
	}
	if manifest.Service != "" {
		if info, err := os.Stat(filepath.Join(folder, manifest.Service)); err != nil || info.Mode()&0o100 == 0 {
			return Manifest{}, errors.New("The package's service can't run.")
		}
	}
	if manifest.Page != "" {
		if info, err := os.Stat(filepath.Join(folder, manifest.Page)); err != nil || info.IsDir() {
			return Manifest{}, errors.New("The package's page is missing.")
		}
	}
	target := m.pending(manifest.Name)
	if err := os.RemoveAll(target); err != nil {
		return Manifest{}, err
	}
	if err := copyTree(folder, target); err != nil {
		_ = os.RemoveAll(target)
		return Manifest{}, err
	}
	return manifest, nil
}

// replaceable keeps a bot from overwriting a plugin that came from an index.
func (m *Manager) replaceable(ctx context.Context, name string) error {
	if !m.known(ctx, name) {
		return nil
	}
	value, ok := m.record(ctx, name)
	if !ok || !strings.HasPrefix(value.Source, LocalSource) {
		return errors.New("A plugin with that name is already installed.")
	}
	return nil
}

// Adopt installs a staged plugin once someone confirms it, and turns it on in the chat that asked.
func (m *Manager) Adopt(ctx context.Context, name, chat, maker string) (contract.Plugin, error) {
	staged := m.pending(name)
	// A repeated answer finds its plugin already installed.
	if _, err := os.Stat(staged); errors.Is(err, os.ErrNotExist) && m.known(ctx, name) {
		return contract.Plugin{}, nil
	}
	manifest, err := readManifest(staged)
	if err != nil {
		return contract.Plugin{}, err
	}
	if err := m.replaceable(ctx, name); err != nil {
		return contract.Plugin{}, err
	}
	m.stopService(name)
	if err := m.swap(name, staged); err != nil {
		return contract.Plugin{}, err
	}
	value := record{Manifest: manifest, State: contract.PluginStateReady, Source: LocalSource + maker,
		Title: manifest.Title, Symbol: manifest.Symbol, Kind: manifest.Kind}
	m.saves.Lock()
	err = m.save(ctx, name, value)
	m.saves.Unlock()
	if err != nil {
		return contract.Plugin{}, err
	}
	m.launch(manifest)
	list, err := m.List(ctx)
	if err != nil {
		return contract.Plugin{}, err
	}
	chats := []string{chat}
	if position := slices.IndexFunc(list.Installed, func(item contract.Plugin) bool { return item.Name == name }); position >= 0 {
		for _, known := range list.Installed[position].Chats {
			if known != chat {
				chats = append(chats, known)
			}
		}
	}
	return m.SetChats(ctx, name, chats)
}

// ServePage answers from the folder holding a web plugin's page, and from nowhere else.
func (m *Manager) ServePage(w http.ResponseWriter, r *http.Request, name, path string) bool {
	value, ok := m.record(r.Context(), name)
	if !ok || value.State != contract.PluginStateReady || value.Manifest.Surface != SurfaceWeb {
		return false
	}
	root, err := os.OpenRoot(filepath.Join(m.home(name), "package", filepath.Dir(value.Manifest.Page)))
	if err != nil {
		return false
	}
	defer root.Close()
	file, err := root.Open(filepath.FromSlash(path))
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	return true
}

// copyTree copies plain files and folders, up to the size of a package.
func copyTree(source, target string) error {
	var total int64
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		into := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(into, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("A local plugin can only hold plain files.")
		}
		if total += info.Size(); total > packageLimit {
			return errors.New("The package is too big.")
		}
		from, err := os.Open(path)
		if err != nil {
			return err
		}
		defer from.Close()
		to, err := os.OpenFile(into, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(to, from); err != nil {
			to.Close()
			return err
		}
		return to.Close()
	})
}
