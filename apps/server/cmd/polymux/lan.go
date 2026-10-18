package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

func listenIP(tailscale bool) (string, error) {
	if tailscale {
		return tailnet()
	}
	return "127.0.0.1", nil
}

var interfaceAddrs = net.InterfaceAddrs

func tailnet() (string, error) {
	addrs, err := interfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("find Tailscale interface: %w", err)
	}
	return tailnetAddr(addrs)
}

func tailnetAddr(addrs []net.Addr) (string, error) {
	prefix := netip.MustParsePrefix("100.64.0.0/10")
	for _, addr := range addrs {
		p, err := netip.ParsePrefix(addr.String())
		if err == nil && prefix.Contains(p.Addr()) {
			return p.Addr().String(), nil
		}
	}
	return "", errors.New("no Tailscale interface found; start Tailscale or omit -tailscale (POLYMUX_TAILSCALE=0)")
}

func privateIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.Is4() {
		return ip.IsPrivate() || ip.IsLinkLocalUnicast() || netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
	}
	return ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

type lanInterface struct {
	Name  string
	Flags net.Flags
	Addrs []net.Addr
}

func lanInterfaces() []lanInterface {
	interfaces, _ := net.Interfaces()
	result := make([]lanInterface, 0, len(interfaces))
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err == nil {
			result = append(result, lanInterface{iface.Name, iface.Flags, addrs})
		}
	}
	return result
}

func addressRank(ip netip.Addr) int {
	ip = ip.Unmap()
	switch {
	case netip.MustParsePrefix("100.64.0.0/10").Contains(ip), netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip):
		return 6
	case netip.MustParsePrefix("192.168.0.0/16").Contains(ip):
		return 1
	case netip.MustParsePrefix("10.0.0.0/8").Contains(ip):
		return 2
	case netip.MustParsePrefix("172.16.0.0/12").Contains(ip):
		return 3
	case ip.Is6() && ip.IsPrivate():
		return 4
	case ip.IsLinkLocalUnicast():
		return 5
	default:
		return 0
	}
}

func interfaceRank(name string) int {
	for _, prefix := range []string{"bridge", "docker", "vmnet", "utun", "vboxnet", "tailscale", "tun", "tap", "veth", "br-"} {
		if strings.HasPrefix(name, prefix) {
			return 2
		}
	}
	for _, prefix := range []string{"en", "wlan", "eth"} {
		if strings.HasPrefix(name, prefix) {
			return 0
		}
	}
	return 1
}

func lanFilter(interfaces []lanInterface, route string, host bool) []string {
	type candidate struct {
		address string
		name    string
		rank    int
	}
	candidates := []candidate{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if host && interfaceRank(iface.Name) == 2 {
			continue
		}
		for _, addr := range iface.Addrs {
			p, err := netip.ParsePrefix(addr.String())
			if err != nil {
				continue
			}
			ip := p.Addr().Unmap()
			rank := addressRank(ip)
			if rank == 0 || ip.IsLoopback() || host && rank == 6 {
				continue
			}
			if ip.Is6() && ip.IsLinkLocalUnicast() {
				ip = ip.WithZone(iface.Name)
			}
			candidates = append(candidates, candidate{ip.String(), iface.Name, rank})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if host {
			if (a.name == route) != (b.name == route) {
				return a.name == route
			}
			if interfaceRank(a.name) != interfaceRank(b.name) {
				return interfaceRank(a.name) < interfaceRank(b.name)
			}
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		return a.address < b.address
	})
	result := []string{}
	seen := map[string]bool{}
	for _, c := range candidates {
		if !seen[c.address] {
			result = append(result, c.address)
			seen[c.address] = true
		}
	}
	return result
}

func defaultRoute() string {
	if runtime.GOOS == "linux" {
		data, _ := os.ReadFile("/proc/net/route")
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 3 && fields[1] == "00000000" {
				flags, err := strconv.ParseUint(fields[3], 16, 32)
				if err == nil && flags&1 != 0 {
					return fields[0]
				}
			}
		}
	}
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		data, _ := exec.CommandContext(ctx, "/sbin/route", "-n", "get", "default").Output()
		fields := strings.Fields(string(data))
		for i, field := range fields {
			if field == "interface:" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	return ""
}

func lanAddrs() []string {
	return lanFilter(lanInterfaces(), "", false)
}

func selectHost(interfaces []lanInterface, route string) string {
	if addrs := lanFilter(interfaces, route, true); len(addrs) > 0 {
		return addrs[0]
	}
	return "127.0.0.1"
}

func lanHost() string {
	return selectHost(lanInterfaces(), defaultRoute())
}

func identityHost(host string) string {
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	if ip.IsUnspecified() {
		ip = netip.MustParseAddr(lanHost())
	}
	// Certificate IP SANs do not include an interface zone.
	return ip.WithZone("").String()
}

func validateListen(address string, _ bool) error { return validateIP(address, false) }
func validateDevice(address string) error         { return validateIP(address, true) }

func validateIP(address string, device bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return errors.New("listener must use an explicit IP address")
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || privateIP(ip) || device && ip.IsUnspecified() {
		return nil
	}
	return errors.New("listener must bind a loopback, private LAN, or Tailscale IP; only the TLS device listener may bind all interfaces")
}
