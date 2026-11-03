package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/packages/vault"
)

const vaultService = "com.polymux.vault"

type hostKeys struct{ Dir string }

func (k hostKeys) Protection() string {
	if runtime.GOOS == "darwin" {
		return "keychain"
	}
	return "owner-only file"
}

func (k hostKeys) account() string {
	dir, _ := filepath.Abs(k.Dir)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(dir)))
}

func (k hostKeys) Load() ([]byte, error) {
	if runtime.GOOS != "darwin" {
		path := filepath.Join(k.Dir, "key")
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, vault.ErrUnavailable
		}
		return os.ReadFile(path)
	}
	data, err := keychain("", "find-generic-password", "-s", vaultService, "-a", k.account(), "-w")
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimSpace(string(data)))
}

func (k hostKeys) Save(key []byte) error {
	if len(key) != 32 {
		return vault.ErrUnavailable
	}
	if runtime.GOOS == "darwin" {
		// The interactive protocol keeps the key out of argv and process listings.
		input := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", vaultService, k.account(), hex.EncodeToString(key))
		_, err := keychain(input, "-i")
		return err
	}
	if err := os.MkdirAll(k.Dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(k.Dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(k.Dir, ".key-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(key)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(k.Dir, "key"))
}

func keychain(input string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/security", args...)
	command.Stdin = strings.NewReader(input)
	data, err := command.Output()
	if err != nil {
		return nil, vault.ErrUnavailable
	}
	return data, nil
}
