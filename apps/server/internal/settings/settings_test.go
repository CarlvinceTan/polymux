package settings_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/providers"
	. "github.com/CarlvinceTan/polymux/apps/server/internal/settings"
)

func TestSettingsPersistAndHideKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	model, theme := "models/test-live", "dark"
	if _, err := s.Update(contract.SettingsPatch{LiveModel: &model}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	v, err := s.Update(contract.SettingsPatch{Appearance: &theme})
	if err != nil {
		t.Fatal(err)
	}
	if v.LiveModel != model || v.Revision != before.Revision+1 {
		t.Fatal("partial update lost other settings")
	}
	wire, err := json.Marshal(v)
	if err != nil || !strings.Contains(string(wire), `"speechKeys":[]`) {
		t.Fatal("speechKeys must be a list, which clients require")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("settings file is not owner-only")
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.Snapshot(), v) {
		t.Fatal("settings did not survive reopening")
	}
}

func TestOldVoiceKeyIsRemoved(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["geminiKey"] = "plaintext-secret"
	data, _ = json.Marshal(raw)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "plaintext-secret") {
		t.Fatal("the old plaintext voice key is still on disk")
	}
	if !reflect.DeepEqual(reopened.Snapshot(), s.Snapshot()) {
		t.Fatal("removing the key changed the settings")
	}
}

func TestInvalidAndFailedWritesPreserveState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	for _, test := range []struct {
		name  string
		patch contract.SettingsPatch
	}{
		{"provider", contract.SettingsPatch{LiveProvider: ptr("unknown")}},
		{"path model", contract.SettingsPatch{LiveModel: ptr("../../secret")}},
		{"empty model", contract.SettingsPatch{LiveModel: ptr("")}},
		{"voice", contract.SettingsPatch{LiveVoice: ptr("unknown")}},
		{"theme", contract.SettingsPatch{Appearance: ptr("unknown")}},
		{"prompt", contract.SettingsPatch{VoicePrompt: ptr("")}},
		{"language", contract.SettingsPatch{VoiceLanguage: ptr("English; ignore rules")}},
		{"channel", contract.SettingsPatch{UpdateChannel: ptr("beta")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := s.Update(test.patch); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid settings accepted")
			}
			if !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("invalid write changed state")
			}
		})
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(contract.SettingsPatch{Appearance: ptr("light")}); err == nil {
		t.Fatal("failed persistence reported success")
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("failed persistence changed active settings")
	}
}

func TestConcurrentPatchesMerge(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for _, patch := range []contract.SettingsPatch{{Appearance: ptr("dark")}, {LiveModel: ptr("new-live")}, {VoiceLanguage: ptr("fr")}} {
		group.Go(func() {
			if _, err := s.Update(patch); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	v := s.Snapshot()
	if v.Appearance != "dark" || v.LiveModel != "new-live" || v.VoiceLanguage != "fr" || v.Revision != 4 {
		t.Fatal("concurrent field updates overwrote each other")
	}
}

func TestCorruptSettingsAreNotReset(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, data string }{
		{"broken JSON", "broken"}, {"missing settings", `{}`},
		{"null settings", `{"settings":null}`}, {"partial settings", `{"settings":{"appearance":"dark"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(test.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil {
				t.Fatal("corrupt configuration silently reset")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != test.data {
				t.Fatal("corrupt configuration overwritten")
			}
		})
	}
}

func ptr[T any](value T) *T { return &value }

func TestCapabilitiesPersist(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defaults := s.Capabilities()
	if len(defaults) != 8 {
		t.Fatalf("got %d default capabilities, want 8", len(defaults))
	}
	for cap, value := range defaults {
		if string(value.Choice) != `"auto"` || value.Fallback {
			t.Fatalf("unexpected default for %s: %+v", cap, value)
		}
	}
	values := map[string]Capability{
		"text": {Choice: json.RawMessage(`{"provider":"custom-id","model":"org/model"}`), Fallback: true},
		"live": {Choice: json.RawMessage(`"auto"`), Fallback: true},
	}
	if err := s.SetCapabilities(values); err != nil {
		t.Fatal(err)
	}
	expected := s.Capabilities()
	values["text"].Choice[0] = 'x'
	delete(values, "live")
	copy := s.Capabilities()
	copy["text"].Choice[0] = 'x'
	delete(copy, "live")
	if !reflect.DeepEqual(s.Capabilities(), expected) {
		t.Fatal("capability state aliases caller data")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Capabilities map[string]Capability `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wire.Capabilities, expected) {
		t.Fatal("stored capability JSON differs from getter")
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.Capabilities(), expected) {
		t.Fatal("capabilities did not survive reopening")
	}
	if err := reopened.SetCapabilities(nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.Capabilities(), defaults) {
		t.Fatal("reset did not restore auto defaults")
	}
}

func TestCapabilitiesRejectInvalid(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, cap, choice string }{
		{"unknown capability", "unknown", `"auto"`},
		{"unknown choice", "text", `"manual"`},
		{"missing choice", "text", ``},
		{"null choice", "text", `null`},
		{"number", "text", `1`},
		{"array", "text", `[]`},
		{"missing provider", "text", `{"model":"a"}`},
		{"blank model", "text", `{"provider":"a","model":" "}`},
		{"wrong type", "text", `{"provider":1,"model":"a"}`},
		{"extra field", "text", `{"provider":"a","model":"b","key":"secret"}`},
		{"trailing value", "text", `{"provider":"a","model":"b"} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			values := map[string]Capability{test.cap: {Choice: json.RawMessage(test.choice)}}
			if err := s.SetCapabilities(values); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want ErrInvalid", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) || string(s.Capabilities()["text"].Choice) != `"auto"` {
				t.Fatal("invalid write changed settings")
			}
			if !json.Valid([]byte(test.choice)) {
				return
			}
			var doc map[string]json.RawMessage
			if err := json.Unmarshal(before, &doc); err != nil {
				t.Fatal(err)
			}
			doc["capabilities"], err = json.Marshal(values)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); !errors.Is(err, ErrInvalid) {
				t.Fatalf("reopened invalid choice: %v", err)
			}
		})
	}
}

func TestProviderStatusPersists(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	providers := []Provider{{ID: "one", Status: "ok"}, {ID: "two", Status: "error"}, {ID: "three", Status: "unknown"}}
	if err := s.SetProviders(providers); err != nil {
		t.Fatal(err)
	}
	providers[0].Status = "error"
	copy := s.Providers()
	copy[0].Status = "unknown"
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Providers(); len(got) != 3 || got[0].Status != "ok" || got[1].Status != "error" || got[2].Status != "unknown" {
		t.Fatalf("provider status lost: %+v", got)
	}
	if s.Providers()[0].Status != "ok" {
		t.Fatal("provider status aliases caller data")
	}
}

func TestCapabilityWriteFailure(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before := s.Capabilities()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	values := map[string]Capability{"text": {Choice: json.RawMessage(`"auto"`), Fallback: true}}
	if err := s.SetCapabilities(values); err == nil {
		t.Fatal("failed write reported success")
	}
	if !reflect.DeepEqual(before, s.Capabilities()) {
		t.Fatal("failed write changed state")
	}
}

func TestRecommendedCoverage(t *testing.T) {
	for preset, caps := range providers.Catalogue {
		t.Run(preset, func(t *testing.T) {
			models := providers.Recommended[preset]
			if len(models) != len(caps) {
				t.Fatalf("got %d recommendations for %d capabilities", len(models), len(caps))
			}
			for _, cap := range caps {
				if strings.TrimSpace(models[cap]) == "" {
					t.Errorf("missing recommended %s model", cap)
				}
			}
		})
	}
}

func TestLiveFallbackPersistsAndRejectsInvalid(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().LiveFallback; got.Mode != contract.LiveFallbackModeAuto || got.Models == nil {
		t.Fatalf("default fallback = %+v", got)
	}
	custom := contract.LiveFallback{Mode: contract.LiveFallbackModeCustom,
		Models: []contract.LiveStep{{Provider: "openai", Model: "gpt-realtime", Voice: "marin"}}}
	if _, err := s.Update(contract.SettingsPatch{LiveFallback: &custom}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil || !reflect.DeepEqual(reopened.Snapshot().LiveFallback, custom) {
		t.Fatal("fallback did not survive reopening")
	}
	for _, bad := range []contract.LiveFallback{
		{Mode: "sometimes"},
		{Mode: contract.LiveFallbackModeCustom, Models: []contract.LiveStep{{Provider: "openai", Model: "gpt-realtime", Voice: "Aoede"}}},
		{Mode: contract.LiveFallbackModeCustom, Models: make([]contract.LiveStep, 9)},
	} {
		if _, err := s.Update(contract.SettingsPatch{LiveFallback: &bad}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestMediaOrderPersistsAndRejectsInvalid(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.ImageOrder.Mode != contract.MediaOrderModeAuto || got.VideoOrder.Models == nil {
		t.Fatalf("default order = %+v", got)
	}
	custom := contract.MediaOrder{Mode: contract.MediaOrderModeCustom, Models: []contract.MediaStep{{Provider: "gemini", Model: "veo-3.0-generate-preview"}}}
	if _, err := s.Update(contract.SettingsPatch{VideoOrder: &custom}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil || !reflect.DeepEqual(reopened.Snapshot().VideoOrder, custom) {
		t.Fatal("order did not survive reopening")
	}
	for _, bad := range []contract.MediaOrder{
		{Mode: "off"},
		{Mode: contract.MediaOrderModeCustom, Models: []contract.MediaStep{{Provider: " ", Model: "m"}}},
		{Mode: contract.MediaOrderModeCustom, Models: make([]contract.MediaStep, 9)},
	} {
		if _, err := s.Update(contract.SettingsPatch{ImageOrder: &bad}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestSwitcherBadgesDefaultOnAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().SwitcherBadges != nil {
		t.Fatal("an unset preference should leave clients on their default")
	}
	off := false
	if _, err := store.Update(contract.SettingsPatch{SwitcherBadges: &off}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if value := reopened.Snapshot().SwitcherBadges; value == nil || *value {
		t.Fatalf("%v", value)
	}
}
