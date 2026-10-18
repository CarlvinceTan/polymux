package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func serverLog(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("secure log directory: %w", err)
	}
	path := filepath.Join(dir, "server.log")
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat server log: %w", err)
	}
	if err == nil {
		if err := os.Chmod(path, 0600); err != nil {
			return nil, fmt.Errorf("secure server log: %w", err)
		}
		if info.Size() > 10<<20 {
			if err := os.Rename(path, path+".1"); err != nil {
				return nil, fmt.Errorf("rotate server log: %w", err)
			}
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open server log: %w", err)
	}
	return file, nil
}
