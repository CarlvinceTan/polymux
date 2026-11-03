//go:build integration

package vault

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func chromeFixture(t *testing.T) (string, string, *cdp) {
	t.Helper()
	binary, _ := exec.LookPath("chromium")
	if runtime.GOOS == "darwin" {
		binary = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skip("Chromium is required")
	}
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><input id="user" autocomplete="username"><input id="pass" type="password"></body></html>`)
	}))
	t.Cleanup(page.Close)
	origin := strings.Replace(page.URL, "127.0.0.1", "localhost", 1)
	profile := t.TempDir()
	command := exec.Command(binary, "--headless=new", "--no-first-run", "--no-default-browser-check",
		"--disable-background-networking", "--disable-sync", "--disable-extensions", "--remote-debugging-port=0",
		"--user-data-dir="+profile, origin)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
	})
	endpoint := ""
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if err == nil {
			endpoint = "http://127.0.0.1:" + strings.Split(string(data), "\n")[0]
			if connection, err := connect(t.Context(), endpoint, origin, ""); err == nil {
				t.Cleanup(func() { connection.socket.Close() })
				waitBrowser(t, connection, `document.readyState === "complete" && !!document.querySelector("#user")`)
				return endpoint, origin, connection
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("headless Chromium was not ready")
	return "", "", nil
}

func evaluate(t *testing.T, connection *cdp, expression string) json.RawMessage {
	t.Helper()
	var result struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := connection.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true}, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Exception) != 0 {
		t.Fatalf("browser fixture script failed: %s", result.Exception)
	}
	return result.Result.Value
}

func TestChromiumFill(t *testing.T) {
	endpoint, origin, connection := chromeFixture(t)
	s, _, _ := testStore(t)
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBrowser(t.Context(), Browser{Endpoint: endpoint}); err != nil {
		t.Fatal(err)
	}
	item := testItem()
	item.URL = origin
	if _, err := s.Save(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	result, err := s.Fill(t.Context(), item.ID, "", nil)
	if err != nil || result.Filled != 2 {
		t.Fatal("real browser fill failed", err)
	}
	var fields []string
	if err := json.Unmarshal(evaluate(t, connection, `[document.querySelector('#user').value,document.querySelector('#pass').value]`), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields[0] != item.Fields["username"] || fields[1] != item.Fields["password"] {
		t.Fatal("browser did not receive stored fields")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), item.Fields["password"]) {
		t.Fatal("browser tool result contains password")
	}
	if err := s.SetBrowser(t.Context(), Browser{Endpoint: "http://example.com:9222"}); err == nil {
		t.Fatal("remote debugging endpoint accepted")
	}
	if err := s.SetBrowser(t.Context(), Browser{Endpoint: endpoint + "/redirect"}); err == nil {
		t.Fatal("debugging endpoint path accepted")
	}
}

func waitBrowser(t *testing.T, connection *cdp, expression string) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		var result struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
		}
		if err := connection.call(t.Context(), "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true}, &result); err == nil && result.Result.Value {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("browser fixture page was not ready")
}

func TestChromiumPasskey(t *testing.T) {
	endpoint, origin, connection := chromeFixture(t)
	ctx := t.Context()
	waitBrowser(t, connection, `typeof navigator.credentials === "object"`)
	if err := connection.call(ctx, "WebAuthn.enable", map[string]any{"enableUI": false}, nil); err != nil {
		t.Fatal(err)
	}
	var authenticator struct {
		ID string `json:"authenticatorId"`
	}
	options := map[string]any{"protocol": "ctap2", "transport": "internal", "hasResidentKey": true, "hasUserVerification": true, "isUserVerified": true, "automaticPresenceSimulation": true}
	if err := connection.call(ctx, "WebAuthn.addVirtualAuthenticator", map[string]any{"options": options}, &authenticator); err != nil {
		t.Fatal(err)
	}
	evaluate(t, connection, `navigator.credentials.create({publicKey:{challenge:new Uint8Array(32).fill(7),rp:{id:'localhost',name:'Vault fixture'},user:{id:new Uint8Array([1,2,3]),name:'test@example.test',displayName:'Test'},pubKeyCredParams:[{type:'public-key',alg:-7}],authenticatorSelection:{residentKey:'required',userVerification:'required'},timeout:10000}}).then(()=>true)`)
	var response struct {
		Credentials []struct {
			ID     string `json:"credentialId"`
			Key    string `json:"privateKey"`
			Handle string `json:"userHandle"`
		} `json:"credentials"`
	}
	if err := connection.call(ctx, "WebAuthn.getCredentials", map[string]any{"authenticatorId": authenticator.ID}, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Credentials) != 1 {
		t.Fatal("fixture did not register a passkey")
	}
	credential := response.Credentials[0]
	key, err := base64.StdEncoding.DecodeString(credential.Key)
	if err != nil {
		t.Fatal(err)
	}
	toURL := func(raw string) string {
		data, _ := base64.StdEncoding.DecodeString(raw)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	s, _, _ := testStore(t)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBrowser(ctx, Browser{Endpoint: endpoint}); err != nil {
		t.Fatal(err)
	}
	item := Item{ID: "passkey", Kind: "passkey", Title: "Fixture passkey", URL: origin, Fields: map[string]string{
		"rpID": "localhost", "credentialID": toURL(credential.ID), "userHandle": toURL(credential.Handle),
		"privateKey": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})),
	}, Codes: []Code{}}
	if _, err := s.Save(ctx, item); err != nil {
		t.Fatal("passkey import failed", err)
	}
	if err := connection.call(ctx, "WebAuthn.removeVirtualAuthenticator", map[string]any{"authenticatorId": authenticator.ID}, nil); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	done := make(chan error, 1)
	work, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	go func() { done <- s.Passkey(work, item.ID, "", func() { close(ready) }) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatal("passkey was not ready", err)
	case <-work.Done():
		t.Fatal("passkey setup timed out")
	}
	result := evaluate(t, connection, `navigator.credentials.get({publicKey:{challenge:new Uint8Array(32).fill(9),rpId:'localhost',userVerification:'required',timeout:10000}}).then(value=>Boolean(value.response.signature))`)
	if string(result) != "true" {
		t.Fatal("real WebAuthn assertion failed")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-work.Done():
		t.Fatal("passkey completion timed out")
	}
	stored, err := s.Get(ctx, item.ID)
	if err != nil || stored.Fields["signCount"] != "1" {
		t.Fatal("sign counter was not persisted", err)
	}
}
