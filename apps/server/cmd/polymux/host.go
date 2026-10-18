package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/host"
)

func runHost(args []string) error {
	if len(args) == 0 || args[0] != "run" && args[0] != "install" {
		return errors.New("usage: polymux-server host run|install [-data path] [-listen IP:7337] [-control IP:7338] [-tailscale]")
	}
	mode := args[0]
	flags := flag.NewFlagSet("host", flag.ContinueOnError)
	dir := flags.String("data", os.Getenv("POLYMUX_DATA_DIR"), "server data directory")
	listen := flags.String("listen", os.Getenv("POLYMUX_LISTEN"), "authenticated backend listener (loopback, private LAN or Tailscale; defaults to 127.0.0.1:7337)")
	control := flags.String("control", "", "unauthenticated controller listener; should stay on loopback (defaults to 127.0.0.1:7338)")
	dev := flags.Bool("dev", false, "development mode (loopback is always allowed)")
	tailscale := flags.Bool("tailscale", os.Getenv("POLYMUX_TAILSCALE") == "1", "use Tailscale listeners (requires Tailscale)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if *dir == "" {
		*dir = filepath.Join(home, "Library", "Application Support", "Polymux", "server")
	}
	*dir, err = filepath.Abs(*dir)
	if err != nil {
		return err
	}
	*listen, *control, err = hostListeners(*listen, *control, *tailscale)
	if err != nil {
		return err
	}
	if err := validateListen(*listen, *dev); err != nil {
		return err
	}
	if err := validateListen(*control, *dev); err != nil {
		return err
	}
	if *listen == *control {
		return errors.New("backend and controller need different ports")
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if mode == "install" {
		if runtime.GOOS != "darwin" {
			return errors.New("host install requires macOS")
		}
		unit, err := host.Unit(binary, *dir, *listen, *control)
		if err != nil {
			return err
		}
		option := "<string>-tailscale=" + strconv.FormatBool(*tailscale) + "</string></array>"
		unit = []byte(strings.Replace(string(unit), "</array>", option, 1))
		path := filepath.Join(home, "Library", "LaunchAgents", host.Label+".plist")
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("host service already exists; do not replace a running installation")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := os.MkdirAll(*dir, 0700); err != nil {
			return err
		}
		if err := os.WriteFile(path, unit, 0600); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "/bin/launchctl", "bootstrap", "gui/"+strconv.Itoa(os.Getuid()), path).Run(); err != nil {
			return fmt.Errorf("service file installed but launchctl bootstrap failed: %w", err)
		}
		fmt.Printf("Host controller installed at %s; settings endpoint http://%s/host\n", path, *control)
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	lock, err := lockData(*dir, ".host.lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	childArgs := []string{"-data", *dir, "-listen", *listen, "-tailscale=" + strconv.FormatBool(*tailscale)}
	if *dev {
		childArgs = append(childArgs, "-dev")
	}
	controller, err := host.Open(host.Config{Binary: binary, Args: childArgs, Dir: *dir})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *control)
	if err != nil {
		return err
	}
	defer listener.Close()
	_, backendPort, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(backendPort)
	if err != nil || n > 65533 {
		return errors.New("backend port must allow a device port")
	}
	httpServer := &http.Server{Handler: pairRoutes(controller, *dir, strconv.Itoa(n+2), *tailscale), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	done := make(chan struct{})
	go func() { defer close(done); controller.Run(ctx) }()
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	select {
	case <-ctx.Done():
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			cancel()
			<-done
			return err
		}
	}
	cancel()
	<-done
	end, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	return httpServer.Shutdown(end)
}

func hostListeners(listen, control string, tailscale bool) (string, string, error) {
	defaultIP, err := listenIP(tailscale)
	if err != nil {
		return "", "", err
	}
	if listen == "" {
		listen = net.JoinHostPort(defaultIP, "7337")
	}
	if control == "" {
		_, rawPort, err := net.SplitHostPort(listen)
		if err != nil {
			return "", "", err
		}
		port, err := strconv.Atoi(rawPort)
		if err != nil || port >= 65535 {
			return "", "", errors.New("backend port must allow an adjacent controller port")
		}
		control = net.JoinHostPort(defaultIP, strconv.Itoa(port+1))
	}
	return listen, control, nil
}
