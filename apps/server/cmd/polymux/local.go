package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
		// A server elsewhere never replaces the token of the install living here.
		if _, err := os.Stat(filepath.Join(clientDir, "polymux.db")); err == nil && filepath.Clean(clientDir) != filepath.Clean(dir) {
			return token, nil
		}
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

// serveProof answers the app's local-server check: an HMAC of its nonce under the owner token
// proves this listener is the real server before the app sends that token. The token never leaves.
func serveProof(w http.ResponseWriter, r *http.Request, owner string) {
	var body struct {
		Nonce string `json:"nonce"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if r.Method != http.MethodPost || owner == "" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Nonce) < 16 || len(body.Nonce) > 128 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Only this machine may ask, and the proof names the port it was asked on, so a proof
	// can't be relayed from the real server to an impostor on another port.
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	host, port, err := net.SplitHostPort(fmt.Sprint(local))
	remote, _, remoteErr := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(remote)
	if err != nil || remoteErr != nil || ip == nil || !ip.IsLoopback() && remote != host {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	mac := hmac.New(sha256.New, []byte(owner))
	mac.Write([]byte("polymux-local-proof:" + port + ":" + body.Nonce))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"proof": hex.EncodeToString(mac.Sum(nil))})
}
