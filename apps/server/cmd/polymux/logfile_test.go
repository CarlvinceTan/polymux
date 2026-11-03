package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerLogFileModeAndRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	open := func() *os.File {
		t.Helper()
		file, err := serverLog(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { file.Close() })
		return file
	}
	file := open()
	slog.New(slog.NewTextHandler(file, nil)).Error("reply failed", "detail", "connection refused")
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "connection refused") {
		t.Fatalf("log: %s %v", data, err)
	}
	for _, tc := range []struct {
		path string
		mode os.FileMode
	}{{path, 0600}, {filepath.Dir(path), 0700}} {
		info, err := os.Stat(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != tc.mode {
			t.Fatalf("mode %s: %o", tc.path, info.Mode().Perm())
		}
	}
	file.Close()
	file = open()
	if _, err := file.WriteString("appended\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	data, err = os.ReadFile(path)
	if err != nil || !strings.HasSuffix(string(data), "appended\n") || !strings.Contains(string(data), "connection refused") {
		t.Fatalf("append: %s %v", data, err)
	}
	if err := os.Truncate(path, 10<<20+1); err != nil {
		t.Fatal(err)
	}
	file = open()
	info, err := file.Stat()
	if err != nil || info.Size() != 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("new log: %v %v", info, err)
	}
	info, err = os.Stat(path + ".1")
	if err != nil || info.Size() != 10<<20+1 {
		t.Fatalf("rotated log: %v %v", info, err)
	}
}
