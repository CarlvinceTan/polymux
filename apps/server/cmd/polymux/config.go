package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

type modelConfig struct {
	URL     string   `json:"url"`
	Model   string   `json:"model"`
	Models  []string `json:"models"`
	KeyEnv  string   `json:"keyEnv"`
	KeyFile string   `json:"keyFile"`
	// Headers are sent with every provider request; Session names one of them to carry a stable per-conversation value.
	Headers map[string]string `json:"headers"`
	Session string            `json:"sessionHeader"`
}

func loadModel(dir string) (modelConfig, error) {
	var config modelConfig
	data, err := os.ReadFile(filepath.Join(dir, "model.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return config, err
	}
	if err == nil {
		if len(data) > 8192 || json.Unmarshal(data, &config) != nil {
			return config, errors.New("invalid model configuration")
		}
	}
	if config.KeyEnv != "" && !regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`).MatchString(config.KeyEnv) {
		return config, errors.New("model keyEnv must name an existing environment variable")
	}
	if config.KeyFile != "" && !filepath.IsAbs(config.KeyFile) {
		return config, errors.New("model keyFile must be an absolute path")
	}
	for name, value := range config.Headers {
		if !validHeaderName(name) {
			return config, fmt.Errorf("invalid model header name %q", name)
		}
		if !validHeaderValue(value) {
			return config, fmt.Errorf("invalid model header value for %q", name)
		}
	}
	if config.Session != "" && !validHeaderName(config.Session) {
		return config, errors.New("model sessionHeader must be a valid header name")
	}
	if value := os.Getenv("POLYMUX_MODEL_URL"); value != "" {
		config.URL = value
	}
	if value := os.Getenv("POLYMUX_MODEL"); value != "" {
		config.Model = value
	}
	if config.Model == "" {
		config.Model = "openai/gpt-5.5"
	}
	if value, present := os.LookupEnv("POLYMUX_MODELS"); present {
		config.Models = strings.Split(value, ",")
	}
	valid := regexp.MustCompile(`^[A-Za-z0-9._/:-]+$`)
	if len(config.Models) > 32 {
		return config, errors.New("invalid model configuration")
	}
	seen := make(map[string]bool)
	models := []string{config.Model}
	seen[config.Model] = true
	for _, id := range config.Models {
		if len(id) > 128 || !valid.MatchString(id) {
			return config, errors.New("invalid model configuration")
		}
		if !seen[id] {
			models = append(models, id)
			seen[id] = true
		}
	}
	config.Models = models
	return config, nil
}

// validHeaderName accepts HTTP field-name tokens, which rules out header smuggling.
func validHeaderName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r) {
			continue
		}
		return false
	}
	return true
}

func validHeaderValue(value string) bool {
	return value != "" && len(value) <= 1024 && !strings.ContainsAny(value, "\r\n")
}

// Key resolves the model credential from the environment first, then an owner-only file.
// The file path exists so a supervised service can carry a key without environment variables.
func (c modelConfig) Key() (string, error) {
	if key := os.Getenv("POLYMUX_MODEL_KEY"); key != "" {
		return key, nil
	}
	if c.KeyEnv != "" {
		if key := os.Getenv(c.KeyEnv); key != "" {
			return key, nil
		}
	}
	if c.KeyFile == "" {
		return "", nil
	}
	info, err := os.Stat(c.KeyFile)
	if err != nil {
		return "", fmt.Errorf("model key file: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("model key file must not be readable by other users")
	}
	data, err := os.ReadFile(c.KeyFile)
	if err != nil {
		return "", fmt.Errorf("model key file: %w", err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" || len(key) > 4096 || strings.ContainsAny(key, "\r\n") {
		return "", errors.New("invalid model key file")
	}
	return key, nil
}

func lockData(dir, name string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another Polymux process owns this data directory")
	}
	return file, nil
}
