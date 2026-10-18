package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
)

func TestLocalPublication(t *testing.T) {
	for _, override := range []bool{false, true} {
		name := "default"
		if override {
			name = "environment"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("POLYMUX_DATA_DIR", "")
			clientDir := filepath.Join(home, ".polymux", "data")
			if override {
				clientDir = filepath.Join(home, "client-data")
				t.Setenv("POLYMUX_DATA_DIR", clientDir)
			}
			// Model production's non-default -data independently of the app environment.
			dir := filepath.Join(home, ".polymux", "data")
			pairs, err := device.Open(t.Context(), dir, "localhost")
			if err != nil {
				t.Fatal(err)
			}
			defer pairs.Close()
			token, err := localCredential(t.Context(), pairs, dir)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(clientDir, "devices", "local-token")
			data, err := os.ReadFile(path)
			if err != nil || string(data) != token {
				t.Fatal("client path must contain the server credential")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("published credential must be owner-only")
			}
			pair, err := pairs.Authenticate(t.Context(), string(data))
			if err != nil {
				t.Fatal(err)
			}
			again, err := localCredential(t.Context(), pairs, dir)
			if err != nil || again != token {
				t.Fatal("restart must preserve the credential")
			}
			if err := pairs.Revoke(t.Context(), pair.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := pairs.Authenticate(t.Context(), string(data)); err == nil {
				t.Fatal("published credential must respect revocation")
			}
			replacement, err := localCredential(t.Context(), pairs, dir)
			if err != nil || replacement == token {
				t.Fatal("restart must publish a fresh credential")
			}
			data, err = os.ReadFile(path)
			if err != nil || string(data) != replacement {
				t.Fatal("client path must contain the replacement credential")
			}
		})
	}
}
