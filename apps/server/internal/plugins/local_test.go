package plugins

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func webPlugin() Manifest {
	return Manifest{Name: "notes", Title: "Notes", Version: "0.1.0", Kind: KindScoped, Symbol: "note", SelectedSymbol: "note.fill",
		Surface: SurfaceWeb, Page: "ui/index.html", HideClose: true}
}

func folder(t *testing.T, value Manifest) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"plugin.json": manifest(t, value), "ui/index.html": "<p>Notes</p>", "ui/app.js": "1", "secret.txt": "no"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func page(m *Manager, name, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/plugins/"+name+"/ui/"+path, nil)
	if !m.ServePage(response, request, name, path) {
		response.Code = http.StatusNotFound
	}
	return response
}

func looseChats(t *testing.T, data *store.Store) {
	t.Helper()
	if _, err := data.DB.ExecContext(t.Context(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
}

func TestLocalPluginNeedsConfirmingAndServesOnlyItsPage(t *testing.T) {
	m, data := manager(t, newMarket(t))
	looseChats(t, data)
	source := folder(t, webPlugin())
	if _, err := m.Stage(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if list, err := m.List(t.Context()); err != nil || len(list.Installed) != 0 {
		t.Fatalf("staging installed something: %+v %v", list, err)
	}
	if page(m, "notes", "index.html").Code != http.StatusNotFound {
		t.Fatal("an unconfirmed plugin served its page")
	}
	if _, err := m.Adopt(t.Context(), "notes", "chat-1", "Aria"); err != nil {
		t.Fatal(err)
	}
	plugin := settled(t, m, "notes")
	if plugin.State != contract.PluginStateReady || plugin.Source != LocalSource+"Aria" || !slices.Equal(plugin.Chats, []string{"chat-1"}) {
		t.Fatalf("installed as %+v", plugin)
	}
	if plugin.Page == nil || *plugin.Page != "index.html" || plugin.HideClose == nil || !*plugin.HideClose {
		t.Fatalf("page details lost: %+v", plugin)
	}
	if body := page(m, "notes", "index.html"); body.Code != http.StatusOK || body.Body.String() != "<p>Notes</p>" {
		t.Fatalf("page: %d %s", body.Code, body.Body)
	}
	for _, path := range []string{"../secret.txt", "../plugin.json", "/etc/hosts"} {
		if page(m, "notes", path).Code != http.StatusNotFound {
			t.Fatalf("served %s from outside the page's folder", path)
		}
	}
	if market, err := m.Market(t.Context()); err != nil || len(market) != 0 {
		t.Fatalf("a local plugin reached the Marketplace: %+v %v", market, err)
	}
}

func TestLocalPluginUpdateKeepsChatsAndData(t *testing.T) {
	m, data := manager(t, newMarket(t))
	looseChats(t, data)
	for _, version := range []string{"0.1.0", "0.2.0"} {
		value := webPlugin()
		value.Version = version
		if _, err := m.Stage(t.Context(), folder(t, value)); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Adopt(t.Context(), "notes", "chat-"+version, "Aria"); err != nil {
			t.Fatal(err)
		}
		if version == "0.1.0" {
			data := filepath.Join(m.home("notes"), "data")
			if err := os.MkdirAll(data, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(data, "kept"), []byte("1"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	plugin := settled(t, m, "notes")
	if plugin.Version != "0.2.0" || len(plugin.Chats) != 2 {
		t.Fatalf("after update: %+v", plugin)
	}
	if _, err := os.Stat(filepath.Join(m.home("notes"), "data", "kept")); err != nil {
		t.Fatal("the update lost the plugin's data")
	}
	if _, err := m.Adopt(t.Context(), "notes", "chat-1", "Aria"); err != nil {
		t.Fatalf("a repeated answer failed: %v", err)
	}
}

func TestLocalPluginRefusals(t *testing.T) {
	m, _ := manager(t, newMarket(t))
	official := record{State: contract.PluginStateReady, Source: "CarlvinceTan/polymux@1"}
	if err := m.save(t.Context(), "hub", official); err != nil {
		t.Fatal(err)
	}
	edit := func(change func(*Manifest)) Manifest {
		value := webPlugin()
		change(&value)
		return value
	}
	for name, value := range map[string]Manifest{
		"gate":      edit(func(v *Manifest) { v.Provides, v.Service = []string{CapabilityGate}, "run" }),
		"transport": edit(func(v *Manifest) { v.Provides, v.Service = []string{CapabilityTransport}, "run" }),
		"surface":   edit(func(v *Manifest) { v.Surface, v.Page = "hub", "" }),
		"no page":   edit(func(v *Manifest) { v.Page = "" }),
		"global":    edit(func(v *Manifest) { v.Kind = KindGlobal }),
		"outside":   edit(func(v *Manifest) { v.Page = "../index.html" }),
		"missing":   edit(func(v *Manifest) { v.Page = "ui/nothing.html" }),
		"no run":    edit(func(v *Manifest) { v.Service = "ui/app.js" }),
		"official":  edit(func(v *Manifest) { v.Name = "hub" }),
	} {
		if _, err := m.Stage(t.Context(), folder(t, value)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	linked := folder(t, webPlugin())
	if err := os.Symlink("/etc/hosts", filepath.Join(linked, "ui", "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Stage(t.Context(), linked); err == nil {
		t.Error("a symlink was accepted")
	}
}
