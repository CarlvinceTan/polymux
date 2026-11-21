package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteURL(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
		invalid           bool
	}{
		{name: "disabled"},
		{name: "secure", input: "https://api.example.com/", want: "https://api.example.com"},
		{name: "http", input: "http://api.example.com", invalid: true},
		{name: "credentials", input: "https://secret@api.example.com", invalid: true},
		{name: "path", input: "https://api.example.com/chat", invalid: true},
		{name: "query", input: "https://api.example.com?token=secret", invalid: true},
		{name: "fragment", input: "https://api.example.com#secret", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := remoteURL(test.input)
			if (err != nil) != test.invalid || got != test.want {
				t.Fatalf("origin = %q, error = %v", got, err)
			}
		})
	}
}

func TestRemoteListener(t *testing.T) {
	for _, address := range []string{"0.0.0.0:0", ":0", "192.168.1.2:0", "100.64.0.1:0"} {
		t.Run(address, func(t *testing.T) {
			listener, err := remoteListener(address)
			if err == nil {
				listener.Close()
				t.Fatal("public tunnel listener accepted a non-loopback bind")
			}
		})
	}
	listener, err := remoteListener("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
}

func TestPublicListenerFallback(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	listener, err := listenerOrFree(held.Addr().String(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if listener.Addr().String() == held.Addr().String() {
		t.Fatal("public listener reused a busy address")
	}
}

func TestRemoteBoundary(t *testing.T) {
	handler := remoteHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer paired-device" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, test := range []struct {
		name, path, token string
		status            int
	}{
		{name: "unauthenticated", path: "/health", status: http.StatusUnauthorized},
		{name: "paired", path: "/health", token: "paired-device", status: http.StatusNoContent},
		{name: "local proof", path: "/local/proof", token: "paired-device", status: http.StatusNotFound},
		{name: "encoded proof", path: "/local%2Fproof", token: "paired-device", status: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, nil)
			request.Host = "localhost"
			request.Header.Set("X-Forwarded-For", "127.0.0.1")
			request.Header.Set("Authorization", "Bearer "+test.token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}
