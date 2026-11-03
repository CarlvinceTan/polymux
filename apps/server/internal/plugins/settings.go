package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
)

const settingsFile = "settings.json"

// SettingValue is a declared setting with its current value.
type SettingValue struct {
	Setting
	Value any `json:"value"`
}

func (m *Manager) settingsPath(name string) string {
	return filepath.Join(m.home(name), "data", settingsFile)
}

// manifestOf returns an installed plugin's manifest.
func (m *Manager) manifestOf(ctx context.Context, name string) (Manifest, error) {
	records, err := m.config.Store.Plugins(ctx)
	if err != nil {
		return Manifest{}, err
	}
	for _, saved := range records {
		var value record
		if saved.Name == name && json.Unmarshal(saved.Data, &value) == nil {
			return value.Manifest, nil
		}
	}
	return Manifest{}, ErrUnknown
}

func (m *Manager) savedSettings(name string) map[string]any {
	saved := map[string]any{}
	if data, err := os.ReadFile(m.settingsPath(name)); err == nil {
		_ = json.Unmarshal(data, &saved)
	}
	return saved
}

// Settings lists a plugin's declared settings with their values, defaults filled in.
func (m *Manager) Settings(ctx context.Context, name string) ([]SettingValue, error) {
	manifest, err := m.manifestOf(ctx, name)
	if err != nil {
		return nil, err
	}
	saved := m.savedSettings(name)
	values := make([]SettingValue, 0, len(manifest.Settings))
	for _, setting := range manifest.Settings {
		value, ok := saved[setting.Key]
		if !ok || !setting.valid(value) {
			value = setting.Default
		}
		values = append(values, SettingValue{Setting: setting, Value: value})
	}
	return values, nil
}

// SetSettings saves values for a plugin's declared settings and restarts its service to apply them.
func (m *Manager) SetSettings(ctx context.Context, name string, values map[string]any) ([]SettingValue, error) {
	manifest, err := m.manifestOf(ctx, name)
	if err != nil {
		return nil, err
	}
	saved := m.savedSettings(name)
	for key, value := range values {
		position := slices.IndexFunc(manifest.Settings, func(setting Setting) bool { return setting.Key == key })
		if position < 0 || !manifest.Settings[position].valid(value) {
			return nil, errors.New("Those settings don't fit this plugin.")
		}
		saved[key] = value
	}
	data, err := json.Marshal(saved)
	if err != nil {
		return nil, err
	}
	path := m.settingsPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(temporary, path); err != nil {
		return nil, err
	}
	m.stopService(name)
	m.launch(manifest)
	return m.Settings(ctx, name)
}
