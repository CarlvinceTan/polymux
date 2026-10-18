package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDataLockPreventsSecondServer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, err := lockData(dir, ".server.lock")
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockData(dir, ".server.lock"); err == nil {
		second.Close()
		t.Fatal("second server acquired the same data")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	file, err = lockData(dir, ".server.lock")
	if err != nil {
		t.Fatal("lock survived process lifetime")
	}
	file.Close()
}
func TestModelConfigPersistsOnlyReferences(t *testing.T) {
	t.Setenv("POLYMUX_MODEL", "")
	t.Setenv("POLYMUX_MODEL_URL", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "model.json")
	if err := os.WriteFile(path, []byte(`{"url":"http://127.0.0.1:8080/v1","model":"local/free","keyEnv":"EXISTING_KEY"}`), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := loadModel(dir)
	if err != nil || config.Model != "local/free" || config.KeyEnv != "EXISTING_KEY" {
		t.Fatalf("config %+v %v", config, err)
	}
	t.Setenv("POLYMUX_MODEL", "local/override")
	config, err = loadModel(dir)
	if err != nil || config.Model != "local/override" {
		t.Fatal("explicit environment did not override file")
	}
}

func TestModelKeyResolvesFromEnvironmentThenOwnerOnlyFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "model.key")
	if err := os.WriteFile(keyPath, []byte("file-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POLYMUX_MODEL_KEY", "")
	t.Setenv("EXISTING_KEY", "")
	config := modelConfig{KeyFile: keyPath}
	key, err := config.Key()
	if err != nil || key != "file-secret" {
		t.Fatalf("file key not resolved: %q %v", key, err)
	}
	config.KeyEnv = "EXISTING_KEY"
	t.Setenv("EXISTING_KEY", "env-secret")
	if key, err = config.Key(); err != nil || key != "env-secret" {
		t.Fatalf("declared environment did not win: %q %v", key, err)
	}
	t.Setenv("POLYMUX_MODEL_KEY", "direct-secret")
	if key, err = config.Key(); err != nil || key != "direct-secret" {
		t.Fatalf("direct environment did not win: %q %v", key, err)
	}
}

func TestModelKeyFileRejectsUnsafeSources(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.key")
	if _, err := (modelConfig{KeyFile: missing}).Key(); err == nil {
		t.Fatal("missing key file accepted")
	}
	readable := filepath.Join(dir, "readable.key")
	if err := os.WriteFile(readable, []byte("shared-secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := (modelConfig{KeyFile: readable}).Key(); err == nil {
		t.Fatal("world-readable key file accepted")
	}
	empty := filepath.Join(dir, "empty.key")
	if err := os.WriteFile(empty, []byte("  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (modelConfig{KeyFile: empty}).Key(); err == nil {
		t.Fatal("empty key file accepted")
	}
	if _, err := (modelConfig{KeyFile: "relative.key"}).Key(); err == nil {
		t.Fatal("relative key file path accepted")
	}
	if _, err := loadModel(writeModel(t, `{"url":"https://example.test/v1","model":"m","keyFile":"model.key"}`)); err == nil {
		t.Fatal("relative keyFile survived load")
	}
}

func TestModelConfigWithoutKeyFileHasNoKey(t *testing.T) {
	t.Setenv("POLYMUX_MODEL_KEY", "")
	t.Setenv("EXISTING_KEY", "")
	key, err := (modelConfig{KeyEnv: "EXISTING_KEY"}).Key()
	if err != nil || key != "" {
		t.Fatalf("absent key reported an error: %q %v", key, err)
	}
}

func writeModel(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadModelModelsList(t *testing.T) {
	t.Setenv("POLYMUX_MODEL", "local/opencode")
	t.Setenv("POLYMUX_MODEL_URL", "")
	dir := writeModel(t, `{"models":["local/opencode","local/second","local/second"]}`)
	for _, tc := range []struct {
		name, override string
		want           []string
	}{
		{"file", "", []string{"local/opencode", "local/second"}},
		{"environment", "local/cursor-agent,local/second", []string{"local/opencode", "local/cursor-agent", "local/second"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("POLYMUX_MODELS", tc.override)
			if tc.override == "" {
				os.Unsetenv("POLYMUX_MODELS")
			}
			config, err := loadModel(dir)
			if err != nil || !reflect.DeepEqual(config.Models, tc.want) {
				t.Fatalf("models = %v, error = %v", config.Models, err)
			}
		})
	}
}

func TestLoadModelRejectsBadModels(t *testing.T) {
	t.Setenv("POLYMUX_MODEL", "local/opencode")
	for _, tc := range []struct {
		name   string
		models []string
	}{
		{"empty", []string{""}}, {"characters", []string{"local/bad model"}},
		{"long", []string{strings.Repeat("a", 129)}}, {"too many", strings.Split(strings.Repeat("m,", 32)+"m", ",")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"models": tc.models})
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("POLYMUX_MODELS", "valid")
			os.Unsetenv("POLYMUX_MODELS")
			if _, err := loadModel(writeModel(t, string(body))); err == nil || err.Error() != "invalid model configuration" {
				t.Fatalf("file error = %v", err)
			}
			t.Setenv("POLYMUX_MODELS", strings.Join(tc.models, ","))
			if _, err := loadModel(t.TempDir()); err == nil || err.Error() != "invalid model configuration" {
				t.Fatalf("env error = %v", err)
			}
		})
	}
}

func TestLoadModelDefaultOnly(t *testing.T) {
	t.Setenv("POLYMUX_MODEL", "")
	t.Setenv("POLYMUX_MODELS", "")
	os.Unsetenv("POLYMUX_MODELS")
	config, err := loadModel(t.TempDir())
	if err != nil || config.Model != "openai/gpt-5.5" || !reflect.DeepEqual(config.Models, []string{config.Model}) {
		t.Fatalf("default config = %+v, error = %v", config, err)
	}
}
