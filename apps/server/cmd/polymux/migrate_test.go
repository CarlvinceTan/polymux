package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExportImportRoundTrip(t *testing.T) {
	source := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(source, "devices"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "devices", "token"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POLYMUX_DATA_DIR", source)
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := exportData(archive); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "data")
	t.Setenv("POLYMUX_DATA_DIR", destination)
	if err := importData(archive, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "devices", "token"))
	if err != nil || string(data) != "secret" {
		t.Fatalf("restored token = %q, %v", data, err)
	}
	if err := importData(archive, false); err == nil {
		t.Fatal("non-empty destination accepted without --force")
	}
}
