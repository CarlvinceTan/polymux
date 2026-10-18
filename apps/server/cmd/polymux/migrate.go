package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func dataDir() (string, error) {
	if value := os.Getenv("POLYMUX_DATA_DIR"); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".polymux", "data"), nil
}

func exportData(path string) error {
	root, err := dataDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("data directory is unavailable: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	archive := gzip.NewWriter(file)
	defer archive.Close()
	writer := tar.NewWriter(archive)
	defer writer.Close()
	return filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil || relative == "." {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if entry.IsDir() {
			header.Name += "/"
		}
		if info.Mode()&os.ModeSocket != 0 {
			return nil
		}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		source, err := os.Open(name)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func importData(path string, force bool) error {
	root, err := dataDir()
	if err != nil {
		return err
	}
	if !force {
		if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
			return errors.New("data directory is not empty; stop the server and pass --force to replace it")
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	archive, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer archive.Close()
	reader := tar.NewReader(archive)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") || strings.Contains(clean, string(filepath.Separator)+"..") {
			return fmt.Errorf("unsafe archive path %q", header.Name)
		}
		target := filepath.Join(root, clean)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, reader); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry %q", header.Name)
		}
	}
}
