package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CarlvinceTan/polymux/apps/server/internal/archive"
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
	if _, err := filepath.EvalSymlinks(root); err != nil {
		return fmt.Errorf("data directory is unavailable: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	return archive.Write(file, root, nil)
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
	return archive.Read(file, root)
}
