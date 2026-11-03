package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// proofRequest arrives at port 7337 from the given caller.
func proofRequest(body, remote string) *http.Request {
	r := httptest.NewRequest("POST", "/local/proof", strings.NewReader(body))
	r.RemoteAddr = remote
	return r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 7337}))
}

func TestLocalProof(t *testing.T) {
	w := httptest.NewRecorder()
	serveProof(w, proofRequest(`{"nonce":"0123456789abcdef0123"}`, "127.0.0.1:5000"), "owner-token")
	var body struct{ Proof string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	mac := hmac.New(sha256.New, []byte("owner-token"))
	mac.Write([]byte("polymux-local-proof:7337:0123456789abcdef0123"))
	if body.Proof != hex.EncodeToString(mac.Sum(nil)) || strings.Contains(w.Body.String(), "owner-token") {
		t.Fatal("proof is wrong or leaks the token")
	}
	short := httptest.NewRecorder()
	serveProof(short, proofRequest(`{"nonce":"x"}`, "127.0.0.1:5000"), "owner-token")
	if short.Code != 400 {
		t.Fatal("short nonce accepted")
	}
	remote := httptest.NewRecorder()
	serveProof(remote, proofRequest(`{"nonce":"0123456789abcdef0123"}`, "192.168.1.20:5000"), "owner-token")
	if remote.Code != 403 {
		t.Fatal("another machine got a proof")
	}
}
