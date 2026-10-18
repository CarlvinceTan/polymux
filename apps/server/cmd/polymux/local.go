package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
)

func localCredential(ctx context.Context, pairs *device.Store, dir string) (string, error) {
	token, err := pairs.Local(ctx, dir)
	if err != nil {
		return "", err
	}
	clientDir := os.Getenv("POLYMUX_DATA_DIR")
	if clientDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		clientDir = filepath.Join(home, ".polymux", "data")
	}
	path := filepath.Join(clientDir, "devices", "local-token")
	if filepath.Clean(path) == filepath.Join(filepath.Clean(dir), "devices", "local-token") {
		return token, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	// Publish atomically with owner-only permissions, even when replacing an old token.
	file, err := os.CreateTemp(filepath.Dir(path), ".local-token-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	_, err = file.WriteString(token)
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return token, os.Rename(file.Name(), path)
}
