package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/fixture"
	"github.com/CarlvinceTan/polymux/apps/server/internal/link"
	"github.com/CarlvinceTan/polymux/apps/server/internal/plugins"
	"github.com/CarlvinceTan/polymux/apps/server/internal/push"
	"github.com/CarlvinceTan/polymux/apps/server/internal/search"
	"github.com/CarlvinceTan/polymux/apps/server/internal/server"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/apps/server/internal/tunnel"
	engine "github.com/CarlvinceTan/polymux/packages/search"
)

var buildSHA = "unknown"

func runServer(args []string) error {
	if len(args) > 0 && args[0] == "device" {
		return runDevice(args[1:])
	}
	flagSet := flag.NewFlagSet("serve", flag.ContinueOnError)
	version := flagSet.Bool("version", false, "print the build commit and exit")
	listen := flagSet.String("listen", os.Getenv("POLYMUX_LISTEN"), "authenticated backend IP:port (loopback; defaults to 127.0.0.1:7337)")
	dev := flagSet.Bool("dev", os.Getenv("POLYMUX_DEV") == "1", "development mode (loopback is always allowed)")
	dir := flagSet.String("data", os.Getenv("POLYMUX_DATA_DIR"), "data directory (defaults to ~/.polymux/data)")
	logDir := flagSet.String("log-dir", os.Getenv("POLYMUX_LOG_DIR"), "log directory (defaults to ~/.polymux/logs)")
	deviceListen := flagSet.String("device-listen", "", "TLS device listener (defaults to backend port + 2)")
	noMDNS := flagSet.Bool("no-mdns", os.Getenv("POLYMUX_NO_MDNS") == "1", "disable Bonjour LAN advertisement")
	noTunnel := flagSet.Bool("no-tunnel", os.Getenv("POLYMUX_TUNNEL") == "0", "disable pairing from other networks over iroh")
	publicURL := flagSet.String("public-url", os.Getenv("POLYMUX_PUBLIC_URL"), "HTTPS origin advertised for remote pairing")
	publicListen := flagSet.String("public-listen", os.Getenv("POLYMUX_PUBLIC_LISTEN"), "loopback listener for the HTTPS tunnel (defaults to 127.0.0.1:7341 with -public-url, else a free port for transport plugins)")
	verify := fixture.Register(flagSet)
	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *version {
		fmt.Println(buildSHA)
		return nil
	}
	origin, err := remoteURL(*publicURL)
	if err != nil {
		return err
	}
	*publicURL = origin
	groups, err := loadGroups()
	if err != nil {
		return err
	}
	if *verify {
		dataDir, addr, err := fixture.Apply(*dir)
		if err != nil {
			return err
		}
		*dir = dataDir
		*listen = addr
		*dev = true
		fmt.Println(addr)
	}
	if *dir == "" {
		value, err := defaultDataDir()
		if err != nil {
			return err
		}
		*dir = value
		*logDir = filepath.Join(filepath.Dir(value), "logs")
	}
	if *logDir == "" {
		value, err := defaultLogDir()
		if err != nil {
			return err
		}
		*logDir = value
	}
	if *listen == "" {
		*listen = "127.0.0.1:7337"
	}
	if err := validateListen(*listen, *dev); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	logFile, err := serverLog(*logDir)
	if err != nil {
		return err
	}
	defer logFile.Close()
	log := slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logFile), nil))
	shellCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	if err := useShellPath(shellCtx); err != nil {
		log.Warn("keeping the service PATH", "error", err)
	}
	stop()
	if err := moveGuard(*dir); err != nil {
		return err
	}
	lock, err := lockData(*dir, ".server.lock")
	if err != nil {
		return err
	}
	lock, err = activateIncoming(ctx, *dir, lock)
	if err != nil {
		return err
	}
	defer lock.Close()
	s, err := store.Open(ctx, filepath.Join(*dir, "polymux.db"))
	if err != nil {
		return err
	}
	defer s.Close()
	_, port, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	if *deviceListen == "" {
		n, err := strconv.Atoi(port)
		if err != nil || n > 65533 {
			return errors.New("backend port must allow two adjacent ports")
		}
		*deviceListen = deviceDefault(n)
	}
	if err := validateDevice(*deviceListen); err != nil {
		return err
	}
	deviceHost, _, err := net.SplitHostPort(*deviceListen)
	if err != nil {
		return err
	}
	pairings, err := device.Open(ctx, *dir, identityHost(deviceHost))
	if err != nil {
		return err
	}
	defer pairings.Close()
	owner, err := localCredential(ctx, pairings, *dir)
	if err != nil {
		return err
	}
	devices := device.New(pairings)
	deviceSocket, err := net.Listen("tcp", *deviceListen)
	if err != nil {
		return err
	}
	defer deviceSocket.Close()
	publicAddress := *publicListen
	if publicAddress == "" && *publicURL == "" {
		publicAddress = "127.0.0.1:0"
	}
	publicSocket, err := remoteListener(publicAddress)
	if err != nil {
		return err
	}
	defer publicSocket.Close()
	deviceServer := &http.Server{Handler: devices, ReadTimeout: 10 * time.Second, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8 << 10}
	defer deviceServer.Close()
	config, err := loadModel(*dir)
	if err != nil {
		return err
	}
	baseURL, model := config.URL, config.Model
	key, err := config.Key()
	if err != nil {
		return err
	}
	storage, closeStorage, err := agentruntime.NewDurableStorage(*dir)
	if err != nil {
		return err
	}
	defer closeStorage()
	runtime, err := agentruntime.NewDurable(ctx, *dir, baseURL, model, key, agentruntime.Client{Headers: config.Headers, Session: config.Session, Window: config.Window}, storage, config.Models...)
	if err != nil {
		return err
	}
	defer runtime.Close()
	definitions := []agentruntime.Definition{}
	if *verify {
		definitions = fixture.Agents(model)
	} else if path := os.Getenv("POLYMUX_AGENTS"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &definitions); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, d := range definitions {
		if d.Agent.ID == "" || seen[d.Agent.ID] || strings.ContainsAny(d.Agent.ID, "/\\.") {
			return fmt.Errorf("agent identifiers must be unique path-safe tokens")
		}
		seen[d.Agent.ID] = true
	}
	backend, stopSearch, err := engine.Start(ctx, engine.Config{
		Llama: os.Getenv("POLYMUX_LLAMA"), Model: os.Getenv("POLYMUX_EMBED_MODEL"),
		Projector: os.Getenv("POLYMUX_EMBED_MMPROJ"), URL: os.Getenv("POLYMUX_EMBED_URL"),
		Extractor: os.Getenv("POLYMUX_EXTRACTOR"), Log: log,
	})
	if err != nil {
		return err
	}
	if stopSearch != nil {
		defer stopSearch()
	}
	index := &search.Index{Store: s, Embedder: backend.Embedder, Extractor: backend.Extractor, Log: log}
	if err := index.PrepareSemantic(ctx); err != nil {
		return err
	}
	if index.Embedder != nil {
		if err := index.Reindex(ctx); err != nil {
			log.Warn("reindex stale vectors", "error", err)
		}
	}
	log.Info("search providers", "semantic", index.Embedder != nil, "extractor", index.Extractor != nil)
	var sender server.PushSender
	if path := os.Getenv("POLYMUX_APNS_KEY_FILE"); path != "" {
		sender, err = push.Open(push.Config{Path: path, KeyID: os.Getenv("POLYMUX_APNS_KEY_ID"), TeamID: os.Getenv("POLYMUX_APNS_TEAM_ID"), Topic: "com.flarehq.polymux.app"})
		if err != nil {
			return err
		}
	}
	var relay *tunnel.Tunnel
	ticket := func(wait time.Duration) string {
		if relay == nil {
			return ""
		}
		return relay.Ticket(wait)
	}
	if *noTunnel {
		ticket = nil
	}
	identity, err := link.LoadIdentity(*dir)
	if err != nil {
		return err
	}
	api, err := server.New(ctx, s, server.Config{
		GroupCaps:   &groups,
		PluginIndex: os.Getenv("POLYMUX_PLUGIN_INDEX"),
		BuildSHA:    buildSHA,
		ServerID:    link.Fingerprint(identity.PublicKey),
		Exit:        cancel,
		Supervised:  supervised(),
		Mover: func(target, drain string, merge bool) error {
			return spawnMover(*dir, *listen, *logDir, target, drain, merge)
		},
		Address:     *listen,
		DataDir:     *dir,
		Model:       model,
		Definitions: definitions,
		Factory:     runtime,
		Index:       index,
		Log:         log,
		Push:        sender,
		Devices:     devices,
		Ticket:      ticket,
		PublicURL:   *publicURL,
		Publish:     plugins.Publish{API: publicSocket.Addr().String(), Device: loopbackAddress(deviceSocket.Addr())},
	})
	if err != nil {
		return err
	}
	defer api.Close()
	if *verify {
		if err := fixture.HoldStream(ctx, s); err != nil {
			return err
		}
	}
	apiHandler := api.Handler()
	httpServer := &http.Server{Addr: *listen, Handler: server.AllowAppOrigins(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		addr, err := netip.ParseAddr(host)
		if err != nil || !trustedPeer(addr) {
			device.Reject(w)
			return
		}
		if r.URL.Path == "/local/proof" {
			serveProof(w, r, owner)
			return
		}
		apiHandler.ServeHTTP(w, r)
	})), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	defer httpServer.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Info("Polymux is listening", "address", listener.Addr(), "data", *dir, "model", model)
	done := make(chan error, 4)
	remote := &http.Server{
		Handler: remoteHandler(apiHandler), ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10,
	}
	defer remote.Close()
	go func() { done <- remote.Serve(publicSocket) }()
	log.Info("HTTPS tunnel listener ready", "address", publicSocket.Addr())
	if !*noTunnel {
		local, err := tunnelListener(listener)
		if err != nil {
			return fmt.Errorf("tunnel listener: %w", err)
		}
		if local != listener {
			defer local.Close()
			go func() { done <- httpServer.Serve(local) }()
		}
		relay = tunnel.New(ctx, *dir, local.Addr().String(), log)
	}
	go func() { done <- httpServer.Serve(listener) }()
	go func() { done <- deviceServer.Serve(tls.NewListener(deviceSocket, pairings.TLSConfig())) }()
	if address := deviceSocket.Addr().(*net.TCPAddr); shouldAdvertise(address.IP, *noMDNS) {
		ips := advertisedIPs(address.IP, lanAddrs())
		if len(ips) == 0 {
			log.Info("Bonjour advertisement skipped: no eligible IP addresses")
		} else {
			stop, err := advertise(ctx, *dir, address.Port, ips)
			if err != nil {
				log.Warn("Bonjour advertisement unavailable", "error", err)
			} else {
				defer stop()
			}
		}
	}
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		end, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Streams and unused client connections can outlast the grace period; close them.
		if err := httpServer.Shutdown(end); err != nil {
			log.Warn("closing connections still open at shutdown", "error", err)
			return httpServer.Close()
		}
	}
	return nil
}

// loopbackAddress is where a process on this host reaches a listener, whatever interface it binds.
func loopbackAddress(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return addr.String()
	}
	if tcp.IP == nil || tcp.IP.IsUnspecified() {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(tcp.Port))
	}
	return tcp.String()
}

func tunnelListener(listener net.Listener) (net.Listener, error) {
	if address, ok := listener.Addr().(*net.TCPAddr); ok && address.IP.IsLoopback() {
		return listener, nil
	}
	// The encrypted tunnel's final hop stays on this host, regardless of the public bind.
	return net.Listen("tcp4", "127.0.0.1:0")
}

func advertise(ctx context.Context, dir string, port int, ips []string) (func(), error) {
	instance, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("bonjour hostname: %w", err)
	}
	identity, err := link.LoadIdentity(dir)
	if err != nil {
		return nil, fmt.Errorf("bonjour identity: %w", err)
	}
	return link.Advertise(ctx, instanceName(instance), port, ips, bonjourTXT(identity.PublicKey))
}

func shouldAdvertise(bind net.IP, noMDNS bool) bool {
	return !noMDNS && !bind.IsLoopback()
}

func advertisedIPs(bind net.IP, addrs []string) []string {
	if len(bind) != 0 && !bind.IsUnspecified() {
		addrs = []string{bind.String()}
	}
	var ips []string
	for _, address := range addrs {
		ip, err := netip.ParseAddr(address)
		if err != nil {
			continue
		}
		ip = ip.Unmap()
		if ip.Is6() && ip.IsLinkLocalUnicast() || addressRank(ip) == 6 {
			continue
		}
		ips = append(ips, ip.WithZone("").String())
	}
	return ips
}

func bonjourTXT(key []byte) map[string]string {
	return map[string]string{"v": "1", "k": link.Fingerprint(key)}
}

func instanceName(hostname string) string {
	name, _, _ := strings.Cut(strings.TrimSuffix(hostname, ".local"), ".")
	if name == "" {
		return "Polymux"
	}
	return name
}

func defaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".polymux", "data"), nil
}

func defaultLogDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".polymux", "logs"), nil
}

// Production default: all interfaces.
func deviceDefault(port int) string {
	if port == 0 {
		return net.JoinHostPort("127.0.0.1", "0")
	}
	return net.JoinHostPort("0.0.0.0", strconv.Itoa(port+2))
}
