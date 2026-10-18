//go:build mdns

package link

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
)

const (
	service = "_polymux._tcp"
	domain  = "local."
	// queryBudget is the original 5 s. Queries are re-sent with RFC 6762 back-off (1 s, 2 s, ...),
	// only when a window expires, never on an unrelated packet.
	queryBudget   = 5 * time.Second
	respondWindow = time.Second
)

// advert is what the advertisement answered for one instance.
type advert struct {
	port int
	txt  []string
	ips  []net.IP
}

// uniqueInstance returns a per-run instance name: lane verifies running in parallel on one
// machine must not advertise, or ask for, the same Bonjour instance.
func uniqueInstance(t *testing.T) string {
	t.Helper()
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("random instance suffix: %v", err)
	}
	return fmt.Sprintf("polymux-test-%d-%s", os.Getpid(), hex.EncodeToString(suffix))
}

// queryAdvert asks for service with the unicast-response bit set, so the answer returns to
// this socket instead of the port-5353 multicast group, and repeats while the budget
// lasts. Unlike the zeroconf resolver, which sends unselected, this chooses the interfaces,
// and a responder answers questions on demand, so the wait is on socket events rather than
// on the responder's announcement schedule. It returns nil when the budget runs out.
// It relies on the responder honouring the QU bit: grandcat/zeroconf's server (used by Advertise)
// replies by unicast to the querier's port when QU is set; a multicast-only reply is never seen here.
func queryAdvert(ctx context.Context, instance string) (*advert, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("multicast query needs a deadline")
	}
	packet, err := queryPacket()
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return nil, fmt.Errorf("multicast query socket: %w", err)
	}
	defer conn.Close()
	name := dns.Fqdn(instance+"."+service) + domain
	group := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	ifaces := multicastIfaces()
	if len(ifaces) == 0 {
		return nil, errors.New("multicast unavailable: no multicast interface to query")
	}
	pc := ipv4.NewPacketConn(conn)
	sent := false
	var lastErr error
	buf := make([]byte, 65536)
	window := respondWindow
	for time.Now().Before(deadline) {
		// One query round per window; the window doubles after each round (1 s, 2 s, 4 s ...).
		for i := range ifaces {
			if err := pc.SetMulticastInterface(&ifaces[i]); err != nil {
				lastErr = err
				continue
			}
			if _, err := pc.WriteTo(packet, nil, group); err != nil {
				lastErr = err
				continue
			}
			sent = true
		}
		wait := time.Now().Add(window)
		if deadline.Before(wait) {
			wait = deadline
		}
		window *= 2
		conn.SetReadDeadline(wait)
		for {
			n, _, _, err := pc.ReadFrom(buf)
			if err != nil {
				if errors.Is(err, os.ErrDeadlineExceeded) {
					break // window over: next round re-sends
				}
				return nil, fmt.Errorf("multicast query read: %w", err)
			}
			var msg dns.Msg
			if err := msg.Unpack(buf[:n]); err != nil {
				continue // unrelated or malformed packet: keep reading, don't re-send
			}
			if answer := readAdvert(&msg, name); answer != nil {
				return answer, nil
			}
		}
	}
	if !sent && lastErr != nil {
		return nil, fmt.Errorf("multicast unavailable: no query reached the group: %w", lastErr)
	}
	return nil, nil
}

// queryPacket builds the browse question with the unicast-response bit (RFC 6762 5.4).
func queryPacket() ([]byte, error) {
	msg := new(dns.Msg)
	msg.Id = dns.Id()
	msg.RecursionDesired = false
	msg.Question = []dns.Question{{
		Name:   dns.Fqdn(service) + domain,
		Qtype:  dns.TypePTR,
		Qclass: dns.ClassINET | 1<<15,
	}}
	return msg.Pack()
}

// readAdvert returns what msg answers about name: nil when it says nothing about that
// instance, which is how answers from a parallel run's advertisement are filtered out.
func readAdvert(msg *dns.Msg, name string) *advert {
	records := append(append([]dns.RR{}, msg.Answer...), msg.Ns...)
	records = append(records, msg.Extra...)
	answer := &advert{}
	found := false
	target := ""
	for _, rr := range records {
		if srv, ok := rr.(*dns.SRV); ok && srv.Hdr.Name == name {
			target = srv.Target
		}
	}
	for _, rr := range records {
		switch record := rr.(type) {
		case *dns.SRV:
			if record.Hdr.Name == name {
				answer.port = int(record.Port)
				found = true
			}
		case *dns.TXT:
			if record.Hdr.Name == name {
				answer.txt = record.Txt
			}
		case *dns.A:
			if record.Hdr.Name == target { // only the instance's own host, not other records in the message
				answer.ips = append(answer.ips, record.A)
			}
		}
	}
	if !found {
		return nil
	}
	return answer
}

// multicastIfaces lists the interfaces a query can be sent over.
func multicastIfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	ifaces := make([]net.Interface, 0, len(all))
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		ifaces = append(ifaces, iface)
	}
	return ifaces
}

func TestAdvertise(t *testing.T) {
	const (
		port     = 7339
		loopback = "127.0.0.1"
	)
	loopbackIP := net.ParseIP(loopback)
	instance := uniqueInstance(t)
	ctx, cancel := context.WithTimeout(context.Background(), queryBudget)
	defer cancel()
	stop, err := Advertise(ctx, instance, port, []string{loopback}, map[string]string{"v": "1", "k": "test-fingerprint"})
	if err != nil {
		if runtime.GOOS == "darwin" {
			t.Fatal(err)
		}
		t.Skipf("multicast advertisement unavailable: %v", err)
	}
	defer stop()
	unavailable := func(message string) {
		t.Helper()
		if runtime.GOOS == "darwin" {
			t.Fatal(message)
		}
		t.Skip(message)
	}
	found, err := queryAdvert(ctx, instance)
	if err != nil {
		unavailable(err.Error())
	}
	if found == nil {
		unavailable(fmt.Sprintf("multicast unavailable: %s.%s not discovered within %s", instance, service+".", queryBudget))
	}
	if found.port != port {
		t.Errorf("port = %d, want %d", found.port, port)
	}
	if !slices.Contains(found.txt, "v=1") || !slices.Contains(found.txt, "k=test-fingerprint") {
		t.Errorf("TXT = %v, want version and fingerprint", found.txt)
	}
	if !slices.ContainsFunc(found.ips, func(ip net.IP) bool { return ip.Equal(loopbackIP) }) {
		t.Errorf("addresses = %v, want %s", found.ips, loopback)
	}
}
