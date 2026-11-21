package hub

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

// settings holds Hub's own choices, shared by every device.
type settings struct {
	path string

	mu    sync.Mutex
	value contract.HubSettings
	// names are chats Hub renamed in rooms an external bridge owns.
	names map[string]string
}

func (s *settings) namesPath() string { return filepath.Join(filepath.Dir(s.path), "names.json") }

func (s *settings) chatNames() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.names)
}

func (s *settings) setName(chat, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := maps.Clone(s.names)
	names[chat] = name
	data, err := json.Marshal(names)
	if err != nil {
		return err
	}
	if err := writePrivate(s.namesPath(), data); err != nil {
		return err
	}
	s.names = names
	return nil
}

func openSettings(dir string) (*settings, error) {
	out := &settings{path: filepath.Join(dir, "settings.json"), names: map[string]string{}}
	if data, err := os.ReadFile(out.namesPath()); err == nil {
		if err := json.Unmarshal(data, &out.names); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	data, err := os.ReadFile(out.path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	return out, json.Unmarshal(data, &out.value)
}

func (s *settings) get() contract.HubSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value
}

func (s *settings) set(value contract.HubSettings) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writePrivate(s.path, data); err != nil {
		return err
	}
	s.value = value
	return nil
}
