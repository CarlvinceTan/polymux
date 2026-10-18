package main

import (
	"net"
	"reflect"
	"testing"
)

func TestLANSelection(t *testing.T) {
	iface := func(name string, flags net.Flags, addresses ...string) lanInterface {
		result := lanInterface{Name: name, Flags: flags}
		for _, address := range addresses {
			ip, network, err := net.ParseCIDR(address)
			if err != nil {
				t.Fatal(err)
			}
			network.IP = ip
			result.Addrs = append(result.Addrs, network)
		}
		return result
	}
	for _, test := range []struct {
		name       string
		interfaces []lanInterface
		route      string
		want       string
		hints      []string
	}{
		{name: "loopback", interfaces: []lanInterface{iface("lo0", net.FlagUp|net.FlagLoopback, "127.0.0.1/8", "192.168.1.1/24")}, want: "127.0.0.1", hints: []string{}},
		{name: "public", interfaces: []lanInterface{iface("en0", net.FlagUp, "8.8.8.8/32", "2001:4860::1/64")}, want: "127.0.0.1", hints: []string{}},
		{name: "docker versus LAN", interfaces: []lanInterface{iface("docker0", net.FlagUp, "172.17.0.1/16"), iface("en0", net.FlagUp, "192.168.1.5/24")}, want: "192.168.1.5", hints: []string{"192.168.1.5", "172.17.0.1"}},
		{name: "tailscale only", interfaces: []lanInterface{iface("en0", net.FlagUp, "100.64.0.5/32", "fd7a:115c:a1e0::1/128")}, want: "127.0.0.1", hints: []string{"100.64.0.5", "fd7a:115c:a1e0::1"}},
		{name: "address ranking", interfaces: []lanInterface{iface("en0", net.FlagUp, "fe80::1/64", "fd00::1/64", "172.16.0.1/16", "10.0.0.1/8", "192.168.0.1/24")}, want: "192.168.0.1", hints: []string{"192.168.0.1", "10.0.0.1", "172.16.0.1", "fd00::1", "fe80::1%en0"}},
		{name: "default route", interfaces: []lanInterface{iface("en0", net.FlagUp, "192.168.0.1/24"), iface("eth0", net.FlagUp, "10.0.0.1/8")}, route: "eth0", want: "10.0.0.1", hints: []string{"192.168.0.1", "10.0.0.1"}},
		{name: "virtual route skipped", interfaces: []lanInterface{iface("utun0", net.FlagUp, "192.168.0.1/24"), iface("en0", net.FlagUp, "10.0.0.1/8")}, route: "utun0", want: "10.0.0.1", hints: []string{"192.168.0.1", "10.0.0.1"}},
		{name: "physical preference", interfaces: []lanInterface{iface("other0", net.FlagUp, "192.168.0.1/24"), iface("wlan0", net.FlagUp, "10.0.0.1/8")}, want: "10.0.0.1", hints: []string{"192.168.0.1", "10.0.0.1"}},
		{name: "down interface", interfaces: []lanInterface{iface("en0", 0, "192.168.0.1/24")}, want: "127.0.0.1", hints: []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := selectHost(test.interfaces, test.route); got != test.want {
				t.Errorf("host = %q, want %q", got, test.want)
			}
			if got := lanFilter(test.interfaces, "", false); !reflect.DeepEqual(got, test.hints) {
				t.Errorf("hints = %v, want %v", got, test.hints)
			}
		})
	}
}
