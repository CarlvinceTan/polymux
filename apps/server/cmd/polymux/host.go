package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/host"
)

func runHost(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: polymux host install|remove|update|forget")
	}
	service, err := host.Open()
	if err != nil {
		return err
	}
	binary, err := selfPath()
	if err != nil {
		return err
	}
	switch args[0] {
	case "install":
		if err := service.Install(binary); err != nil {
			return err
		}
		return waitForServer(hostAddress(), 30*time.Second)
	case "remove":
		return service.Remove()
	case "update":
		return updateHost(service, binary)
	case "forget":
		return forgetMoved()
	}
	return fmt.Errorf("unknown host command %q", args[0])
}

func selfPath() (string, error) {
	binary, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(binary)
}

// hostAddress is where the launchd job listens.
func hostAddress() string {
	if listen := os.Getenv("POLYMUX_LISTEN"); listen != "" {
		return "http://" + listen
	}
	return "http://127.0.0.1:7337"
}

// waitForServer treats any HTTP reply as up: /health itself needs a credential.
func waitForServer(address string, limit time.Duration) error {
	client := &http.Client{Timeout: 2 * time.Second}
	for end := time.Now().Add(limit); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if response, err := client.Get(address + "/health"); err == nil {
			response.Body.Close()
			return nil
		}
	}
	return errors.New("the server didn't start; see its launchd log")
}

// updateHost moves a server the app runs onto this copy of it, after an app update or move. A
// server installed any other way has its own updater, so it's left alone.
func updateHost(service host.Service, binary string) error {
	if !service.Installed() {
		return nil
	}
	current, err := service.Binary()
	if err != nil || !strings.Contains(current, ".app/Contents/Helpers/") {
		return err
	}
	address := hostAddress()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token, err := hostToken()
	if err != nil {
		return err
	}
	running, err := maintenanceCall(ctx, address, token, "", "")
	if err != nil {
		return err
	}
	if current == binary && running.Build == buildSHA {
		return nil
	}
	mark := make([]byte, 16)
	if _, err := rand.Read(mark); err != nil {
		return err
	}
	id := hex.EncodeToString(mark)
	if err := drain([]string{"--url", address, "--token-file", tokenPath(), "--id", id}); err != nil {
		return err
	}
	if current == binary {
		err = service.Restart()
	} else {
		err = service.Install(binary)
	}
	if err != nil {
		return err
	}
	if err := waitForServer(address, 30*time.Second); err != nil {
		return err
	}
	return drain([]string{"--url", address, "--token-file", tokenPath(), "--id", id, "--cancel"})
}

// supervised says whether launchd or systemd will start this server again when it exits.
func supervised() bool {
	service, err := host.Open()
	return err == nil && os.Getenv("XPC_SERVICE_NAME") == service.Label || os.Getenv("INVOCATION_ID") != ""
}

func tokenPath() string {
	if dir := os.Getenv("POLYMUX_DATA_DIR"); dir != "" {
		return filepath.Join(dir, "devices", "local-token")
	}
	return ""
}

func hostToken() (string, error) { return readToken(tokenPath()) }
