package main

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
)

func remoteURL(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", errors.New("-public-url must be an HTTPS origin")
	}
	origin := u.Scheme == "https" && u.Hostname() != "" && u.User == nil
	bare := (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == ""
	if !origin || !bare || u.Opaque != "" || u.ForceQuery {
		return "", errors.New("-public-url must be an HTTPS origin without credentials, path, query or fragment")
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

func remoteListener(address string) (net.Listener, error) {
	if address == "" {
		address = "127.0.0.1:7341"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("-public-listen must bind a loopback IP:port")
	}
	return net.Listen("tcp", address)
}

// publicListener keeps the tunnel's port steady across restarts, so a tunnel
// fixed to it keeps reaching the server. Another instance may hold it.
func publicListener(address string) (net.Listener, error) {
	if address != "" {
		return remoteListener(address)
	}
	return listenerOrFree("127.0.0.1:7341", "127.0.0.1:0")
}

func listenerOrFree(preferred, fallback string) (net.Listener, error) {
	listener, err := remoteListener(preferred)
	if err == nil || !errors.Is(err, syscall.EADDRINUSE) {
		return listener, err
	}
	return remoteListener(fallback)
}

// Local proofs must never be reachable through a public tunnel, even with a forged Host.
func remoteHandler(api http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/local" || strings.HasPrefix(r.URL.Path, "/local/") {
			http.NotFound(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
}
