package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/packages/vault"
)

// vaultKeys keeps tests out of the login Keychain.
type vaultKeys struct{ value []byte }

func (k *vaultKeys) Load() ([]byte, error)   { return slices.Clone(k.value), nil }
func (k *vaultKeys) Save(value []byte) error { k.value = slices.Clone(value); return nil }
func (*vaultKeys) Protection() string        { return "test" }

func testVault(t *testing.T, s *Server) {
	t.Helper()
	s.vault.Close()
	var err error
	s.vault, err = vault.Open(t.Context(), t.TempDir(), &vaultKeys{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVaultManagement(t *testing.T) {
	s := settingsAPI(t)
	testVault(t, s)
	dir := t.TempDir()
	pairs, err := device.Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer pairs.Close()
	token, err := pairs.Local(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	s.config.Devices = device.New(pairs)
	api := httptest.NewServer(s.Handler())
	defer api.Close()
	call := func(method, path, body, authorization, origin string, status int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, api.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if authorization != "" {
			req.Header.Set("Authorization", "Bearer "+authorization)
		}
		req.Header.Set("Origin", origin)
		response, err := api.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if response.StatusCode != status {
			t.Fatalf("%s %s: got %d, want %d", method, path, response.StatusCode, status)
		}
		if authorization != "" && status != 404 && response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("vault response is cacheable")
		}
		return data
	}
	call("GET", "/vault/items", "", "", "", 401)
	call("GET", "/vault", "", token, "", 200)
	// Sync is off until a folder is set; a new synced file needs a password, never echoed back.
	folder := t.TempDir()
	if got := call("GET", "/vault/sync", "", token, "", 200); !strings.Contains(string(got), `"state":"off"`) {
		t.Fatal("sync is not off by default", string(got))
	}
	call("PUT", "/vault/sync", `{"location":"relative"}`, token, "", 400)
	call("PUT", "/vault/sync", `{"location":"`+folder+`","password":"short"}`, token, "", 400)
	call("PUT", "/vault/sync", `{"location":"`+folder+`/missing/deeper"}`, token, "", 409)
	synced := call("PUT", "/vault/sync", `{"location":"`+folder+`","password":"sync-route-password"}`, token, "", 200)
	if !strings.Contains(string(synced), `"state":"synced"`) || strings.Contains(string(synced), "sync-route-password") {
		t.Fatal("sync was not set up, or echoed its password", string(synced))
	}
	if _, err := os.Stat(filepath.Join(folder, vault.FileName)); err != nil {
		t.Fatal("no vault file in the sync folder", err)
	}
	call("PUT", "/vault/sync", `{"location":""}`, token, "", 200)
	item := contract.VaultItem{ID: "private", Kind: "login", Title: "Example", URL: "https://example.com", Fields: map[string]string{"password": "vault-private-value", "username": "vault-private-user"}, Codes: []contract.VaultCode{{ID: "code", Value: "vault-private-recovery"}}}
	body, _ := json.Marshal(item)
	call("PUT", "/vault/items/private", string(body), "", "", 401)
	call("PUT", "/vault/items/private", string(body), token, "https://untrusted.example", 403)
	written := call("PUT", "/vault/items/private", string(body), token, "", 200)
	editor := call("GET", "/vault/items/private", "", token, "", 200)
	metadata := call("GET", "/vault/items", "", token, "", 200)
	for _, response := range [][]byte{written, editor, metadata} {
		for _, secret := range []string{"vault-private-value", "vault-private-user", "vault-private-recovery"} {
			if strings.Contains(string(response), secret) {
				t.Fatal("vault returned a saved secret")
			}
		}
	}
	if err := json.Unmarshal(editor, &item); err != nil {
		t.Fatal(err)
	}
	if item.Fields["password"] != "" || item.Codes[0].Value != "" {
		t.Fatal("editor contains saved values")
	}
	item.Title = "Renamed"
	body, _ = json.Marshal(item)
	updated := call("PUT", "/vault/items/private", string(body), token, "", 200)
	stored, err := s.vault.Get(t.Context(), "private")
	if err != nil || stored.Fields["password"] != "vault-private-value" || stored.Codes[0].Value != "vault-private-recovery" {
		t.Fatal("rename lost hidden values", err)
	}
	if err := json.Unmarshal(updated, &item); err != nil {
		t.Fatal(err)
	}
	item.Fields["password"] = "replacement-value"
	item.Removed = []string{"username"}
	body, _ = json.Marshal(item)
	call("PUT", "/vault/items/private", string(body), token, "", 200)
	stored, err = s.vault.Get(t.Context(), "private")
	if err != nil || stored.Fields["password"] != "replacement-value" || stored.Fields["username"] != "" {
		t.Fatal("replacement or clear failed", err)
	}
	for _, path := range []string{"/vault/setup", "/vault/unlock", "/vault/password", "/vault/renew", "/vault/lock", "/vault/cards/card/password", "/vault/cards/card/complete"} {
		call("POST", path, `{}`, token, "", 404)
	}
	call("GET", "/vault/items/private/totp", "", token, "", 404)
	call("DELETE", "/vault/items/private?revision=3", "", "", "", 401)
	call("DELETE", "/vault/items/private?revision=3", "", token, "", 204)
}

func TestVaultTransport(t *testing.T) {
	s := settingsAPI(t)
	testVault(t, s)
	dir := t.TempDir()
	pairs, err := device.Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer pairs.Close()
	token, err := pairs.Local(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	s.config.Devices = device.New(pairs)
	for _, test := range []struct {
		name   string
		peer   string
		tls    bool
		status int
	}{
		{name: "local tunnel hop", peer: "127.0.0.1:1234", status: 200},
		{name: "local IPv6", peer: "[::1]:1234", status: 200},
		{name: "plain tailnet", peer: "100.64.0.7:1234", status: 403},
		{name: "plain LAN", peer: "192.168.1.5:1234", status: 403},
		{name: "TLS", peer: "192.168.1.5:1234", tls: true, status: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/vault", nil)
			request.RemoteAddr = test.peer
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("X-Polymux-Client", "polymux-kit")
			request.Header.Set("X-Forwarded-Proto", "https")
			request.Header.Set("X-Forwarded-For", "127.0.0.1")
			if test.tls {
				request.TLS = &tls.ConnectionState{}
			}
			response := httptest.NewRecorder()
			s.Handler().ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestVaultAgentFlow(t *testing.T) {
	const secret = "vault-agent-credential-never-in-context"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("credential not injected")
		}
		fmt.Fprint(w, secret)
	}))
	defer origin.Close()
	factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		for _, name := range []string{"vault_list", "vault_request"} {
			args := `{}`
			if name == "vault_request" {
				raw, _ := json.Marshal(vault.Request{ID: "api-entry", URL: origin.URL, Method: "GET", Auth: "bearer"})
				args = string(raw)
			}
			result, err := botCall(ctx, d, name, args)
			if err != nil {
				return agentruntime.Result{}, err
			}
			if strings.Contains(result.Text(), secret) {
				t.Error("vault tool returned secret to model")
			}
		}
		return agentruntime.Result{Text: "Vault request completed"}, nil
	}}
	s, _ := startTest(t, factory)
	testVault(t, s)
	_, err := s.vault.Save(t.Context(), vault.Item{ID: "api-entry", Title: "Service", Kind: "api", URL: origin.URL, Fields: map[string]string{"token": secret}, Codes: []vault.Code{}})
	if err != nil {
		t.Fatal(err)
	}
	botSend(t, s, "assistant", "vault-flow", "Use the saved service credential")
	awaitRun(t, s.Store, store.NewRun("vault-flow", "assistant", "assistant", "message").ID, "completed")
	botReply(t, s, "assistant", "Vault request completed")
	messages, err := s.Store.Messages(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(messages)
	if strings.Contains(string(data), secret) {
		t.Fatal("agent persisted credential")
	}
}

func TestVaultPasskeyFailureReason(t *testing.T) {
	s := settingsAPI(t)
	testVault(t, s)
	if err := s.vault.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.startPasskey(t.Context(), "owner", "missing", ""); !errors.Is(err, vault.ErrMissing) {
		t.Fatal("passkey failure reason was hidden", err)
	}
}
