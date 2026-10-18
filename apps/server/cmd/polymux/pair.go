package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
)

type invite struct {
	URL    string `json:"url"`
	Token  string `json:"token"`
	Ticket string `json:"ticket,omitempty"`
}

func pair(args []string) error {
	flags := flag.NewFlagSet("pair", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("url", "", "server base URL (defaults to this Mac's Tailscale or LAN address)")
	tokenFile := flags.String("token-file", "", "path to the server owner token")
	noQR := flags.Bool("no-qr", false, "print only the link")
	if err := flags.Parse(args); err != nil {
		return errors.New("usage: polymux pair [--url URL] [--token-file PATH] [--no-qr]")
	}
	address := strings.TrimRight(*base, "/")
	if address == "" {
		address = defaultPairURL()
	}
	token, err := readToken(*tokenFile)
	if err != nil {
		return err
	}
	pairing, err := fetchInvite(address, token)
	if err != nil {
		return err
	}
	link := connectLink(pairing)
	fmt.Println("Pairing link:")
	fmt.Println(link)
	if !*noQR {
		fmt.Println()
		qrterminal.GenerateHalfBlock(link, qrterminal.L, os.Stdout)
	}
	fmt.Println()
	fmt.Println("Scan with Polymux, or copy the link into Settings > Devices.")
	return nil
}

func readToken(path string) (string, error) {
	if path == "" {
		path = os.Getenv("POLYMUX_TOKEN_FILE")
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".polymux", "data", "devices", "local-token")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read owner token: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("owner token is empty")
	}
	return token, nil
}

func fetchInvite(base, token string) (invite, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	request, err := http.NewRequest(http.MethodGet, base+"/network/invite", nil)
	if err != nil {
		return invite{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return invite{}, fmt.Errorf("reach %s: %w", base, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return invite{}, fmt.Errorf("pairing request returned HTTP %d", response.StatusCode)
	}
	var value invite
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		return invite{}, err
	}
	if value.URL == "" || value.Token == "" {
		return invite{}, errors.New("server returned an incomplete pairing link")
	}
	return value, nil
}

func connectLink(value invite) string {
	parts := url.URL{Scheme: "polymux", Host: "connect"}
	query := url.Values{}
	query.Set("url", value.URL)
	query.Set("token", value.Token)
	if value.Ticket != "" {
		query.Set("ticket", value.Ticket)
	}
	parts.RawQuery = query.Encode()
	return parts.String()
}

func defaultPairURL() string {
	addresses, err := net.InterfaceAddrs()
	if err == nil {
		tailnet := netip.MustParsePrefix("100.64.0.0/10")
		var candidates []net.IP
		for _, address := range addresses {
			ip, _, splitErr := net.ParseCIDR(address.String())
			if splitErr != nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			if addr, ok := netip.AddrFromSlice(ip.To4()); ok && tailnet.Contains(addr) {
				return "http://" + ip.String() + ":7337"
			}
			if ip.IsPrivate() {
				candidates = append(candidates, ip)
			}
		}
		if len(candidates) > 0 {
			return "http://" + candidates[0].String() + ":7337"
		}
	}
	return "http://127.0.0.1:7337"
}
