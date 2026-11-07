package main

import (
	"archive/tar"
	"compress/gzip"
	"net"
	"os"
	"path/filepath"
	"syscall"
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

func TestExportImportSymlinks(t *testing.T) {
	outside := t.TempDir()
	source := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	write(t, filepath.Join(source, "agents", "a", "tool", "cli.js"), "script")
	bin := filepath.Join(source, "agents", "a", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	kept := map[string]string{"file": "../tool/cli.js", "dir": "../tool", "dangling": "missing"}
	for name, target := range kept {
		if err := os.Symlink(target, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	skipped := map[string]string{"absolute": outside, "relative": "../../../../outside", "self": "self"}
	for name, target := range skipped {
		if err := os.Symlink(target, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
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
	restored := filepath.Join(destination, "agents", "a", "bin")
	for name, target := range kept {
		if got, err := os.Readlink(filepath.Join(restored, name)); err != nil || got != target {
			t.Fatalf("%s links to %q, %v", name, got, err)
		}
	}
	for name := range skipped {
		if _, err := os.Lstat(filepath.Join(restored, name)); !os.IsNotExist(err) {
			t.Fatalf("%s was restored: %v", name, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(restored, "file"))
	if err != nil || string(data) != "script" {
		t.Fatalf("read through link = %q, %v", data, err)
	}
	if err := importData(archive, true); err != nil {
		t.Fatalf("second import over existing links: %v", err)
	}
}

func TestExportLinkedRoot(t *testing.T) {
	parent := t.TempDir()
	write(t, filepath.Join(parent, "real", "devices", "token"), "secret")
	linked := filepath.Join(parent, "linked")
	if err := os.Symlink(filepath.Join(parent, "real"), linked); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POLYMUX_DATA_DIR", linked)
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := exportData(archive); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "data")
	t.Setenv("POLYMUX_DATA_DIR", destination)
	if err := importData(archive, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "devices", "token")); err != nil || string(data) != "secret" {
		t.Fatalf("restored token = %q, %v", data, err)
	}
}

func TestExportSkipsSpecialFiles(t *testing.T) {
	source := t.TempDir()
	write(t, filepath.Join(source, "kept"), "data")
	// A relative address keeps the socket path under the platform limit.
	t.Chdir(source)
	listener, err := net.Listen("unix", "agent.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := syscall.Mkfifo("pipe", 0o600); err != nil {
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
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 1 || entries[0].Name() != "kept" {
		t.Fatalf("restored entries = %v, %v", entries, err)
	}
}

func TestExportImportFileDetails(t *testing.T) {
	source := t.TempDir()
	write(t, filepath.Join(source, "a", "..hidden"), "dots")
	write(t, filepath.Join(source, "script"), "#!/bin/sh")
	write(t, filepath.Join(source, "shared"), "plain")
	if err := os.Chmod(filepath.Join(source, "script"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(source, "shared"), 0o644); err != nil {
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
	if data, err := os.ReadFile(filepath.Join(destination, "a", "..hidden")); err != nil || string(data) != "dots" {
		t.Fatalf("dotted name = %q, %v", data, err)
	}
	for name, want := range map[string]os.FileMode{"script": 0o700, "shared": 0o600} {
		info, err := os.Stat(filepath.Join(destination, name))
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s mode = %v, %v", name, info, err)
		}
	}
}

func TestImportRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"../outside", "a/../../outside", "/outside", "."} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "data")
			t.Setenv("POLYMUX_DATA_DIR", root)
			header := tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o600, Size: int64(len("pwned"))}
			if err := importData(writeArchive(t, []tar.Header{header}), false); err == nil {
				t.Fatal("unsafe path accepted")
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatalf("data directory = %v, %v", entries, err)
			}
		})
	}
}

func TestImportRejectsEscapingLinks(t *testing.T) {
	link := func(name, target string) tar.Header {
		return tar.Header{Typeflag: tar.TypeSymlink, Name: name, Linkname: target}
	}
	file := func(name string) tar.Header {
		return tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o600, Size: int64(len("pwned"))}
	}
	cases := []struct {
		name    string
		entries func(outside string) []tar.Header
	}{
		{"absolute", func(outside string) []tar.Header { return []tar.Header{link("out", outside)} }},
		{"relative", func(string) []tar.Header { return []tar.Header{link("a/out", "../../outside")} }},
		{"through another link", func(string) []tar.Header {
			return []tar.Header{link("a/b/up", "../.."), link("a/b/out", "up/..")}
		}},
		{"file under link", func(outside string) []tar.Header {
			return []tar.Header{link("out", outside), file("out/pwned")}
		}},
		{"file under later link", func(outside string) []tar.Header {
			return []tar.Header{file("out/pwned"), link("out", outside)}
		}},
		{"link replacing a used directory", func(string) []tar.Header {
			return []tar.Header{link("a/b/up", "../.."), link("a", "b"), link("a/b/out", "up/..")}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			parent := t.TempDir()
			outside := filepath.Join(parent, "outside")
			if err := os.Mkdir(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(parent, "data")
			t.Setenv("POLYMUX_DATA_DIR", root)
			if err := importData(writeArchive(t, test.entries(outside)), false); err == nil {
				t.Fatal("escaping archive accepted")
			}
			if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
				t.Fatalf("outside directory = %v, %v", entries, err)
			}
			assertNoLinks(t, root)
		})
	}
}

func TestForceImportIgnoresExistingLinks(t *testing.T) {
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim")
	write(t, victim, "original")
	root := t.TempDir()
	if err := os.Symlink(victim, filepath.Join(root, "file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POLYMUX_DATA_DIR", root)
	header := tar.Header{Typeflag: tar.TypeReg, Name: "file", Mode: 0o600, Size: int64(len("pwned"))}
	if err := importData(writeArchive(t, []tar.Header{header}), true); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(root, "file")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("file was not replaced: %v, %v", info, err)
	}
	header.Name = "dir/pwned"
	if err := importData(writeArchive(t, []tar.Header{header}), true); err == nil {
		t.Fatal("wrote through an existing escaping link")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 1 {
		t.Fatalf("outside directory = %v, %v", entries, err)
	}
	if data, err := os.ReadFile(victim); err != nil || string(data) != "original" {
		t.Fatalf("victim = %q, %v", data, err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeArchive fills every regular entry with "pwned".
func writeArchive(t *testing.T, headers []tar.Header) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	archive := gzip.NewWriter(file)
	defer archive.Close()
	writer := tar.NewWriter(archive)
	defer writer.Close()
	for _, header := range headers {
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := writer.Write([]byte("pwned")); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}

func assertNoLinks(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.Type()&os.ModeSymlink != 0 {
			t.Errorf("link left behind: %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
