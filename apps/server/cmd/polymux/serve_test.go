package main

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

type addressedListener struct {
	net.Listener
	address *net.TCPAddr
}

func (l addressedListener) Addr() net.Addr { return l.address }

func TestTunnelListener(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "100.64.0.7", "192.168.1.5", "0.0.0.0", "::"} {
		t.Run(address, func(t *testing.T) {
			primary, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer primary.Close()
			bound := primary.Addr().(*net.TCPAddr)
			listener := addressedListener{Listener: primary, address: &net.TCPAddr{IP: net.ParseIP(address), Port: bound.Port}}
			local, err := tunnelListener(listener)
			if err != nil {
				t.Fatal(err)
			}
			defer local.Close()
			if !local.Addr().(*net.TCPAddr).IP.IsLoopback() {
				t.Fatalf("encrypted tunnel forwards to non-loopback %s", local.Addr())
			}
			api := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				host, _, _ := net.SplitHostPort(r.RemoteAddr)
				if !net.ParseIP(host).IsLoopback() {
					t.Error("tunnel hop left loopback")
				}
				if r.Header.Get("Authorization") != "Bearer test-device" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})}
			defer api.Close()
			done := make(chan error, 1)
			go func() { done <- api.Serve(local) }()
			client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
			defer client.CloseIdleConnections()
			for _, authorized := range []bool{false, true} {
				request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+local.Addr().String(), nil)
				if err != nil {
					t.Fatal(err)
				}
				want := http.StatusUnauthorized
				if authorized {
					request.Header.Set("Authorization", "Bearer test-device")
					want = http.StatusNoContent
				}
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, response.Body)
				response.Body.Close()
				if response.StatusCode != want {
					t.Fatalf("status = %d, want %d", response.StatusCode, want)
				}
			}
			if err := api.Shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != http.ErrServerClosed {
				t.Fatalf("listener shutdown: %v", err)
			}
		})
	}
}

func TestNetworkBoundary(t *testing.T) {
	for _, test := range []struct {
		address string
		allowed bool
		device  bool
	}{
		{address: "100.127.193.111:7337", allowed: true},
		{address: "[fd7a:115c:a1e0::1]:7337", allowed: true},
		{address: "0.0.0.0:7337"},
		{address: ":7337"},
		{address: "192.168.1.5:7337"},
		{address: "10.0.0.1:7337"},
		{address: "172.16.0.1:7337"},
		{address: "127.0.0.1:7337", allowed: true},
		{address: "[::1]:7337", allowed: true},
		{address: "8.8.8.8:7337"},
		{address: "[2001:4860:4860::8888]:7337"},
		{address: "[fd00::1]:7339", allowed: true, device: true},
		{address: "[fe80::1%en0]:7339", allowed: true, device: true},
		{address: "0.0.0.0:7339", allowed: true, device: true},
		{address: "[::]:7339", allowed: true, device: true},
		{address: "192.168.1.5:7339", allowed: true, device: true},
		{address: "100.64.0.1:7339", allowed: true, device: true},
		{address: "8.8.8.8:7339", device: true},
		{address: "[fc00::1]:7339", allowed: true, device: true},
		{address: "[::ffff:8.8.8.8]:7339", device: true},
	} {
		t.Run(test.address, func(t *testing.T) {
			for _, dev := range []bool{false, true} {
				err := validateListen(test.address, dev)
				if test.device {
					err = validateDevice(test.address)
				}
				if (err == nil) != test.allowed {
					t.Errorf("dev=%v: %v", dev, err)
				}
			}
		})
	}
}

// Plain HTTP reaches only loopback and Tailscale peers; a LAN numbered from Tailscale's range stays LAN.
func TestTrustedPeers(t *testing.T) {
	interfaces := []lanInterface{
		{Name: "en0", Addrs: []net.Addr{&net.IPNet{IP: net.ParseIP("100.70.0.2"), Mask: net.CIDRMask(24, 32)}}},
		{Name: "utun4", Addrs: []net.Addr{&net.IPNet{IP: net.ParseIP("100.101.1.1"), Mask: net.CIDRMask(32, 32)}}},
	}
	for address, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "100.101.1.1": true, "100.88.4.2": true, "fd7a:115c:a1e0::9": true,
		"192.168.1.20": false, "10.0.0.4": false, "100.70.0.9": false, "8.8.8.8": false,
	} {
		if trustedPeer(netip.MustParseAddr(address), interfaces) != want {
			t.Errorf("%s trusted = %v", address, !want)
		}
	}
}

func TestLANAddrs(t *testing.T) {
	addrs := lanAddrs()
	for i := 1; i < len(addrs); i++ {
		if addressRank(netip.MustParseAddr(addrs[i-1])) > addressRank(netip.MustParseAddr(addrs[i])) {
			t.Fatal("LAN addresses are not ranked")
		}
	}
	for _, address := range addrs {
		ip, err := netip.ParseAddr(address)
		if err != nil || ip.IsLoopback() || !privateIP(ip) {
			t.Errorf("invalid LAN address %q", address)
		}
	}
}

func TestListenerDefaults(t *testing.T) {
	ip, err := listenIP(false)
	if err != nil || ip != "127.0.0.1" {
		t.Fatalf("default listener IP = %q, %v", ip, err)
	}
	if err := validateDevice("0.0.0.0:7339"); err != nil {
		t.Fatal(err)
	}
	if host := identityHost("0.0.0.0"); host != identityHost(lanHost()) {
		t.Fatalf("identity host = %q", host)
	}
	if got := identityHost("fe80::1%en0"); got != "fe80::1" {
		t.Fatalf("identity zone was retained: %q", got)
	}
}

func TestTailscaleAbsent(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		&net.IPNet{IP: net.ParseIP("192.168.1.5"), Mask: net.CIDRMask(24, 32)},
	}
	if _, err := tailnetAddr(addrs); err == nil || !strings.Contains(err.Error(), "no Tailscale interface found") {
		t.Fatalf("missing Tailscale error = %v", err)
	}
}

func TestTailscalePresent(t *testing.T) {
	addrs := []net.Addr{&net.IPNet{IP: net.ParseIP("100.64.0.5"), Mask: net.CIDRMask(32, 32)}}
	if ip, err := tailnetAddr(addrs); err != nil || ip != "100.64.0.5" {
		t.Fatalf("Tailscale IP = %q, %v", ip, err)
	}
}

func TestTailscaleCommands(t *testing.T) {
	previous := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) { return []net.Addr{}, nil }
	t.Cleanup(func() { interfaceAddrs = previous })
	t.Setenv("POLYMUX_TAILSCALE", "0")
	t.Setenv("POLYMUX_LISTEN", "")
	for _, command := range []string{"run", "host", "device"} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			invoke := func(tailscale bool) error {
				args := []string{"-data", dir}
				if tailscale {
					args = append(args, "-tailscale")
				}
				switch command {
				case "host":
					return runHost(append([]string{"run"}, append(args, "-listen", "8.8.8.8:7337")...))
				case "device":
					return runDevice(append([]string{"list"}, args...))
				default:
					return runServer(args)
				}
			}
			err := invoke(true)
			if err == nil || !strings.Contains(err.Error(), "no Tailscale interface found") {
				t.Fatalf("-tailscale error = %v", err)
			}
			t.Setenv("POLYMUX_TAILSCALE", "1")
			err = invoke(false)
			if err == nil || !strings.Contains(err.Error(), "no Tailscale interface found") {
				t.Fatalf("POLYMUX_TAILSCALE=1 error = %v", err)
			}
			t.Setenv("POLYMUX_TAILSCALE", "0")
			if command == "run" {
				// Stop after listener selection, before startup can reach external services.
				dir = filepath.Join(dir, "blocked")
				if err := os.WriteFile(dir, []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err = invoke(false)
			if err != nil && strings.Contains(err.Error(), "no Tailscale interface found") {
				t.Fatalf("default still requires Tailscale: %v", err)
			}
			if command == "device" && err != nil {
				t.Fatal(err)
			}
			if command == "run" && err == nil {
				t.Fatal("expected startup to stop at blocked data directory")
			}
		})
	}
}

func TestInstanceName(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		want     string
	}{
		{name: "local hostname", hostname: "cvmini.local", want: "cvmini"},
		{name: "bare hostname", hostname: "cvmini", want: "cvmini"},
		{name: "qualified hostname", hostname: "cvmini.example.local", want: "cvmini"},
		{name: "empty hostname", hostname: "", want: "Polymux"},
		{name: "empty label", hostname: ".local", want: "Polymux"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := instanceName(tt.hostname); got != tt.want {
				t.Errorf("instanceName(%q) = %q, want %q", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestAdvertisedIPs(t *testing.T) {
	addrs := []string{"192.168.1.5", "fd00::1", "100.64.0.5", "fd7a:115c:a1e0::1", "fe80::1%en0", "fe80::2", "invalid"}
	for _, tt := range []struct {
		name      string
		bind      string
		addrs     []string
		tailscale bool
		want      []string
	}{
		{name: "tailscale excluded", bind: "0.0.0.0", addrs: addrs, want: []string{"192.168.1.5", "fd00::1"}},
		{name: "tailscale included", bind: "::", addrs: addrs, tailscale: true, want: []string{"192.168.1.5", "fd00::1", "100.64.0.5", "fd7a:115c:a1e0::1"}},
		{name: "specific bind only", bind: "192.168.1.8", addrs: addrs, want: []string{"192.168.1.8"}},
		{name: "specific IPv6", bind: "fd00::2", addrs: addrs, want: []string{"fd00::2"}},
		{name: "specific tailscale excluded", bind: "100.64.0.5", addrs: addrs},
		{name: "specific tailscale included", bind: "100.64.0.5", addrs: addrs, tailscale: true, want: []string{"100.64.0.5"}},
		{name: "specific link local excluded", bind: "fe80::1", addrs: addrs, tailscale: true},
		{name: "all filtered", bind: "0.0.0.0", addrs: []string{"100.64.0.5", "fd7a:115c:a1e0::1", "fe80::1%en0"}},
		{name: "empty", bind: "0.0.0.0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := advertisedIPs(net.ParseIP(tt.bind), tt.addrs, tt.tailscale); !slices.Equal(got, tt.want) {
				t.Errorf("advertisedIPs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldAdvertise(t *testing.T) {
	for _, tt := range []struct {
		name   string
		bind   string
		noMDNS bool
		want   bool
	}{
		{name: "disabled", bind: "0.0.0.0", noMDNS: true},
		{name: "IPv4 loopback", bind: "127.0.0.1"},
		{name: "IPv6 loopback", bind: "::1"},
		{name: "unspecified", bind: "0.0.0.0", want: true},
		{name: "specific", bind: "192.168.1.5", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldAdvertise(net.ParseIP(tt.bind), tt.noMDNS); got != tt.want {
				t.Errorf("shouldAdvertise = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBonjourTXT(t *testing.T) {
	key := []byte("test public key")
	txt := bonjourTXT(key)
	fingerprint := sha256.Sum256(key)
	want := base64.RawURLEncoding.EncodeToString(fingerprint[:])[:16]
	if len(txt) != 2 || txt["v"] != "1" || len(txt["k"]) != 16 || txt["k"] != want {
		t.Fatalf("TXT = %v, want only v=1 and k=%s", txt, want)
	}
}

func TestDeviceDefault(t *testing.T) {
	// An explicit tailnet -listen with no -device-listen must not drag the device listener to loopback.
	_, port, err := net.SplitHostPort("100.64.0.7:8787")
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name      string
		tailscale bool
		tailnet   string
		port      int
		want      string
	}{
		{name: "all interfaces", port: n, want: "0.0.0.0:8789"},
		{name: "tailnet IP", tailscale: true, tailnet: "100.64.0.7", port: n, want: "100.64.0.7:8789"},
		{name: "verify", port: 0, want: "127.0.0.1:0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := deviceDefault(tt.tailscale, tt.tailnet, tt.port)
			if got != tt.want {
				t.Fatalf("device listener = %q, want %q", got, tt.want)
			}
			host, _, err := net.SplitHostPort(got)
			if err != nil {
				t.Fatal(err)
			}
			ip := net.ParseIP(host)
			if ip == nil || ip.IsLoopback() != (tt.port == 0) {
				t.Fatalf("device listener %q has unexpected loopback status", got)
			}
			if got := shouldAdvertise(ip, false); got != (tt.port != 0) {
				t.Fatalf("Bonjour advertisement = %v, want %v", got, tt.port != 0)
			}
		})
	}
}
