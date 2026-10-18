package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const Service = "com.polymux.provider"

type Keychain struct{ Dir string }

func NewKeychain(dir string) *Keychain {
	if runtime.GOOS != "darwin" {
		log.Print("WARNING: provider secrets use an owner-only file fallback")
	}
	return &Keychain{Dir: dir}
}

func (k *Keychain) file(id string) string {
	return filepath.Join(k.Dir, fmt.Sprintf("%x", sha256.Sum256([]byte(Service+"/"+id))))
}

func security(input string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("provider keychain operation failed")
	}
	return strings.TrimSpace(string(out)), nil
}

func (k *Keychain) Set(id, key string) error {
	if runtime.GOOS == "darwin" {
		// Hex keeps secrets and account IDs out of the command parser and argv.
		if strings.ContainsAny(id, "\"\\\r\n") {
			return fmt.Errorf("invalid keychain account")
		}
		_, err := security(fmt.Sprintf("add-generic-password -U -s %s -a \"%s\" -w \"%s\"\n", Service, id, hex.EncodeToString([]byte(key))), "-i")
		return err
	}
	// Off-macOS hosts have no Keychain; the owner-only file is the documented fallback.
	if err := os.MkdirAll(k.Dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(k.file(id), []byte(key), 0600)
}

func (k *Keychain) Get(id string) (string, error) {
	if runtime.GOOS == "darwin" {
		value, err := security("", "find-generic-password", "-s", Service, "-a", id, "-w")
		if err != nil {
			return "", err
		}
		data, err := hex.DecodeString(value)
		return string(data), err
	}
	data, err := os.ReadFile(k.file(id))
	return string(data), err
}

func (k *Keychain) Delete(id string) error {
	if runtime.GOOS == "darwin" {
		_, err := security("", "delete-generic-password", "-s", Service, "-a", id)
		return err
	}
	return os.Remove(k.file(id))
}
