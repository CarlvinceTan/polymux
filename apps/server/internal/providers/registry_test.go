package providers

import (
	"bytes"
	"fmt"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

type fakeStore map[string]string

func (f fakeStore) Set(id, key string) error      { f[Service+"/"+id] = key; return nil }
func (f fakeStore) Get(id string) (string, error) { return f[Service+"/"+id], nil }
func (f fakeStore) Delete(id string) error        { delete(f, Service+"/"+id); return nil }
func TestP2KeysNeverPersisted(t *testing.T) {
	dir := t.TempDir()
	s, err := settings.Open(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := fakeStore{}
	r := Registry{s, f}
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	want := []Provider{{ID: "p2", Type: "openai", Name: "Test", BaseURL: "https://example.test/v1", Enabled: true, Order: 7, Headers: map[string]string{"X-Test": "yes"}, SessionHeader: "X-Session", Models: []Model{{ID: "text", Tags: []string{"text", "vision"}, Enabled: true}, {ID: "off", Enabled: false}}}, {ID: "disabled", Order: 1}}
	if err := r.Save(want); err != nil {
		t.Fatal(err)
	}
	const key = "p2-secret-never-persist"
	if err := r.SetKey("p2", key); err != nil {
		t.Fatal(err)
	}
	if f["com.polymux.provider/p2"] != key {
		t.Fatal("key missing from secret store")
	}
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if bytes.Contains(data, []byte(key)) {
			t.Error("secret leaked to data directory")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(logs.Bytes(), []byte(key)) {
		t.Fatal("secret leaked to logs")
	}
	reopened, err := settings.Open(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.Providers(), want) {
		t.Fatal("metadata did not round-trip")
	}
	got := s.Providers()
	got[0].Models[0].Tags[0] = "changed"
	if !reflect.DeepEqual(s.Providers(), want) {
		t.Fatal("snapshot aliases state")
	}
}
func TestRegistryValidation(t *testing.T) {
	s, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := Registry{s, fakeStore{}}
	for _, p := range [][]Provider{{{ID: ""}}, {{ID: "a"}, {ID: "a"}}, {{ID: "a", Headers: map[string]string{"Authorization": "secret"}}}} {
		if r.Save(p) == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	if r.SetKey("missing", "secret") == nil {
		t.Fatal("unknown provider accepted")
	}
}
func TestRegistryRealKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS Keychain only")
	}
	k := NewKeychain(t.TempDir())
	id := fmt.Sprintf("polymux-test-%d", os.Getpid())
	if err := k.Set(id, "test-key"); err != nil {
		t.Fatal(err)
	}
	defer k.Delete(id)
	if key, err := k.Get(id); err != nil || key != "test-key" {
		t.Fatal("keychain round-trip failed")
	}
	if err := k.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get(id); err == nil {
		t.Fatal("deleted key still readable")
	}
}
