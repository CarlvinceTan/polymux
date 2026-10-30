package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/proxy"
	"github.com/CarlvinceTan/polymux/apps/server/internal/secrets"
	"github.com/CarlvinceTan/polymux/apps/server/internal/sshagent"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"golang.org/x/crypto/ssh"
	sshclient "golang.org/x/crypto/ssh/agent"
)

func TestCredentialFlow(t *testing.T) {
	const secret = "credential-test-never-in-chat"
	f := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, m contract.Message) (agentruntime.Result, error) {
		if strings.HasPrefix(m.ID, "credential-answer:") {
			return agentruntime.Result{Text: "Credential ready"}, nil
		}
		_, err := botCall(ctx, d, "request_secret", `{"name":"service","label":"Service API key","origin":"https://api.example.com"}`)
		return agentruntime.Result{Text: "NO_RESPONSE"}, err
	}}
	api, _ := startTest(t, f)
	botSend(t, api, "assistant", "credential-request", "Connect the service")
	awaitRun(t, api.Store, store.NewRun("credential-request", "assistant", "assistant", "message").ID, "completed")
	messages, err := api.Store.Messages(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var card contract.Message
	for _, m := range messages {
		if len(m.Parts) == 1 && m.Parts[0].Kind == "credential" {
			card = m
		}
	}
	if card.ID == "" {
		t.Fatal("credential card missing")
	}
	for range 2 {
		r := httptest.NewRecorder()
		api.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages/"+card.ID+"/credential", strings.NewReader(`{"value":"`+secret+`","skip":false}`)))
		if r.Code != 200 || strings.Contains(r.Body.String(), secret) {
			t.Fatal("credential submission failed or leaked", r.Code)
		}
	}
	botReply(t, api, "assistant", "Credential ready")
	value, err := api.credential(t.Context(), "assistant", "service", "https://api.example.com/path")
	if err != nil || value != secret {
		t.Fatal("credential unavailable", err)
	}
	if _, err := api.credential(t.Context(), "assistant", "service", "https://other.example.com"); err == nil {
		t.Fatal("credential origin not enforced")
	}
	var count int
	if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs WHERE id=?`, "credential-answer:"+card.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate credential continuation", count, err)
	}
	for _, table := range []string{"messages", "events", "jobs"} {
		if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE `+map[string]string{"messages": "data", "events": "data", "jobs": "request"}[table]+` LIKE ?`, "%"+secret+"%").Scan(&count); err != nil || count != 0 {
			t.Fatal("credential leaked into", table, count, err)
		}
	}
	r := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/threads/assistant/messages/"+card.ID+"/credential", strings.NewReader(`{"skip":true}`))
	request.Header.Set("Origin", "https://untrusted.example")
	api.ServeHTTP(r, request)
	if r.Code != 403 {
		t.Fatal("browser credential write accepted", r.Code)
	}
}

func TestCredentialHTTP(t *testing.T) {
	const value = "http-test-credential"
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Add(1)
		}
		fmt.Fprint(w, "unexpected")
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+value {
			http.Error(w, "missing credential", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, http.StatusFound)
			return
		}
		fmt.Fprint(w, "echo "+value)
	}))
	defer origin.Close()
	api, _ := startTest(t, &voiceFactory{})
	if err := api.secrets.Save("assistant", secrets.Credential{Name: "service", Origin: origin.URL, Header: "Authorization", Scheme: "Bearer", Value: value}); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: store.NewRun("test", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &httpTool{server: api}
	for _, path := range []string{"/echo", "/redirect"} {
		args, _ := json.Marshal(map[string]string{"url": origin.URL + path, "credential": "service"})
		result, err := tool.Execute(ctx, "test", args, nil)
		if err != nil || strings.Contains(result.Text(), value) {
			t.Fatal("request failed or exposed credential", err)
		}
		if path == "/redirect" && !strings.Contains(result.Text(), `"status":302`) {
			t.Fatal("redirect followed", result.Text())
		}
	}
	if leaked.Load() != 0 {
		t.Fatal("credential sent across origins")
	}
}

func TestSecretReferences(t *testing.T) {
	const key, password = "reference-test-api-key", "reference-test-vault-password"
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer other.Close()
	var seen atomic.Value
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		username, secret, _ := r.BasicAuth()
		seen.Store([]string{r.URL.Query().Get("key"), r.Header.Get("X-Api-Key"), string(data), username + ":" + secret})
		// Echo everything back, as some APIs do, so redaction is exercised.
		fmt.Fprintf(w, `{"query":%q,"header":%q,"body":%q,"auth":%q}`, r.URL.RawQuery, r.Header.Get("X-Api-Key"), data, r.Header.Get("Authorization"))
	}))
	defer origin.Close()
	api, _ := startTest(t, &voiceFactory{})
	testVault(t, api)
	if err := api.vault.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	item := contract.VaultItem{ID: "service-login", Kind: "login", Title: "Service", URL: origin.URL, Fields: map[string]string{"username": "owner", "password": password}, Codes: []contract.VaultCode{}}
	if _, err := api.vault.Save(t.Context(), vaultItem(item)); err != nil {
		t.Fatal(err)
	}
	if err := api.secrets.Save("assistant", secrets.Credential{Name: "service", Origin: origin.URL, Header: "Authorization", Scheme: "Bearer", Value: key}); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: store.NewRun("test", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &httpTool{server: api}
	call := func(input map[string]any) (string, error) {
		args, _ := json.Marshal(input)
		result, err := tool.Execute(ctx, "test", args, nil)
		return result.Text(), err
	}
	text, err := call(map[string]any{
		"url": origin.URL + "/echo?key={{secret:service}}", "method": "POST",
		"headers": map[string]string{"X-Api-Key": "{{secret:service}}"},
		"body":    `{"token":"{{secret:service}}"}`,
		"basic":   "{{vault:service-login:username}}:{{vault:service-login:password}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := seen.Load().([]string)
	if got[0] != key || got[1] != key || got[2] != `{"token":"`+key+`"}` || got[3] != "owner:"+password {
		t.Fatalf("references were not filled: %q", got)
	}
	if strings.Contains(text, key) || strings.Contains(text, password) || !strings.Contains(text, "[redacted]") {
		t.Fatal("a filled value reached the agent", text)
	}
	if strings.Contains(text, "b3duZXI6") {
		t.Fatal("encoded basic credentials reached the agent", text)
	}
	for _, input := range []map[string]any{
		{"url": other.URL + "/?key={{secret:service}}"},
		{"url": other.URL, "headers": map[string]string{"X-Api-Key": "{{vault:service-login:password}}"}},
		{"url": origin.URL, "body": "{{secret:missing}}"},
	} {
		if _, err := call(input); err == nil {
			t.Fatalf("an unusable reference was sent: %v", input)
		}
	}
	if leaked.Load() != 0 {
		t.Fatal("a secret reference was sent to another origin")
	}
}

func TestSecretProxy(t *testing.T) {
	const key = "proxy-server-test-key"
	var seen atomic.Value
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.Header.Get("X-Api-Key"))
		fmt.Fprint(w, "echo "+r.Header.Get("X-Api-Key"))
	}))
	defer origin.Close()
	api, _ := startTest(t, &voiceFactory{})
	var err error
	if api.proxy, err = proxy.Start(t.TempDir(), api.fillSecrets, api.boundSecrets); err != nil {
		t.Fatal(err)
	}
	if err := api.secrets.Save("assistant", secrets.Credential{Name: "service", Origin: origin.URL, Header: "Authorization", Scheme: "Bearer", Value: key}); err != nil {
		t.Fatal(err)
	}
	send := func(agent string) string {
		address, _ := url.Parse(api.proxy.Env(agent)["HTTP_PROXY"])
		client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(address)}}
		request, _ := http.NewRequest("GET", origin.URL, nil)
		request.Header.Set("X-Api-Key", "{{secret:service}}")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return string(data)
	}
	if reply := send("assistant"); seen.Load() != key || reply != "echo [redacted]" {
		t.Fatalf("command reference not filled or reply exposed it: %q", reply)
	}
	// Another bot's commands can't use this bot's secret.
	if reply := send("other"); seen.Load() != "{{secret:service}}" || strings.Contains(reply, key) {
		t.Fatalf("another bot used this secret: %q", reply)
	}
}

// requestCard runs request_secret in a direct-chat turn and returns the card it posted.
func requestCard(t *testing.T, api *Server, args map[string]any) string {
	t.Helper()
	state := &turnState{run: store.NewRun("test", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&credentialTool{server: api, action: "request"}).Execute(ctx, "call", raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	var posted struct {
		ID string `json:"message_id"`
	}
	if err := json.Unmarshal([]byte(result.Text()), &posted); err != nil || posted.ID == "" {
		t.Fatal("no card posted", result.Text(), err)
	}
	return posted.ID
}

func answerCredentialCard(t *testing.T, api *Server, card, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages/"+card+"/credential", strings.NewReader(body)))
	return recorder
}

// TestSecretProxyServesEveryBoundOrigin covers a tool that spans hosts, such as gh.
func TestSecretProxyServesEveryBoundOrigin(t *testing.T) {
	const key = "multi-origin-proxy-key"
	var seen []string
	serve := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = append(seen, name+":"+r.Header.Get("X-Api-Key"))
			fmt.Fprint(w, "ok")
		}))
	}
	api, other := serve("api"), serve("other")
	defer api.Close()
	defer other.Close()
	server, _ := startTest(t, &voiceFactory{})
	var err error
	if server.proxy, err = proxy.Start(t.TempDir(), server.fillSecrets, server.boundSecrets); err != nil {
		t.Fatal(err)
	}
	if err := server.secrets.Save("assistant", secrets.Credential{Name: "gh", Origin: api.URL, Origins: []string{other.URL}, Header: "X-Api-Key", Value: key}); err != nil {
		t.Fatal(err)
	}
	address, _ := url.Parse(server.proxy.Env("assistant")["HTTP_PROXY"])
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(address)}}
	for _, target := range []string{api.URL, other.URL} {
		request, _ := http.NewRequest("GET", target, nil)
		request.Header.Set("X-Api-Key", "{{secret:gh}}")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	if len(seen) != 2 || seen[0] != "api:"+key || seen[1] != "other:"+key {
		t.Fatal("a bound origin did not receive the secret", seen)
	}
}

func TestEphemeralCredential(t *testing.T) {
	const value = "ephemeral-credential-value"
	api, _ := startTest(t, &voiceFactory{})
	card := requestCard(t, api, map[string]any{"name": "scratch", "label": "One task key", "origin": "https://api.example.com", "ttlMinutes": 5, "ephemeral": true})
	if recorder := answerCredentialCard(t, api, card, `{"value":"`+value+`"}`); recorder.Code != 200 || strings.Contains(recorder.Body.String(), value) {
		t.Fatal("ephemeral credential not saved or leaked", recorder.Code, recorder.Body.String())
	}
	var count int
	if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM credentials`).Scan(&count); err != nil || count != 0 {
		t.Fatal("ephemeral credential reached the database", count, err)
	}
	saved, err := api.secrets.Get("assistant", "scratch")
	if err != nil || saved.Value != value || !saved.Ephemeral || saved.ExpiresAt == 0 {
		t.Fatal("ephemeral credential unavailable", saved, err)
	}
	for _, listed := range api.secrets.List("assistant") {
		if listed.Name == "scratch" && listed.Value != "" {
			t.Fatal("listed credential exposed its value")
		}
	}
}

func TestSSHKeyCredential(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "deploy")
	if err != nil {
		t.Fatal(err)
	}
	key := string(pem.EncodeToMemory(block))
	api, _ := startTest(t, &voiceFactory{})
	card := requestCard(t, api, map[string]any{"name": "deploy", "kind": "ssh", "label": "Deploy key", "origin": "github.com"})
	if recorder := answerCredentialCard(t, api, card, `{"value":"not a key"}`); recorder.Code != 400 {
		t.Fatal("invalid SSH key accepted", recorder.Code)
	}
	body, _ := json.Marshal(map[string]string{"value": key})
	if recorder := answerCredentialCard(t, api, card, string(body)); recorder.Code != 200 || strings.Contains(recorder.Body.String(), "PRIVATE KEY") {
		t.Fatal("SSH key not saved or leaked", recorder.Code, recorder.Body.String())
	}
	keys := api.sshKeys("assistant")
	if len(keys) != 1 || keys[0].Comment != "deploy" {
		t.Fatal("saved key missing from the agent", len(keys))
	}
	if _, err := api.credential(t.Context(), "assistant", "deploy", "https://github.com"); err == nil {
		t.Fatal("SSH key usable as an HTTP credential")
	}
	if listed := api.secrets.List("assistant"); len(listed) != 1 || listed[0].Kind != secrets.KindSSH || listed[0].Value != "" {
		t.Fatal("listed SSH key exposed its value", listed)
	}
}

func TestShellEnvCarriesTheAgentSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "deploy")
	if err != nil {
		t.Fatal(err)
	}
	api, _ := startTest(t, &voiceFactory{})
	if api.ssh, err = sshagent.Start(api.sshKeys); err != nil {
		t.Fatal(err)
	}
	if err := api.secrets.Save("assistant", secrets.Credential{Name: "deploy", Kind: secrets.KindSSH, Origin: "github.com", Value: string(pem.EncodeToMemory(block))}); err != nil {
		t.Fatal(err)
	}
	path := api.shellEnv("assistant")["SSH_AUTH_SOCK"]
	if path == "" {
		t.Fatal("commands got no SSH agent socket")
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	listed, err := sshclient.NewClient(conn).List()
	if err != nil || len(listed) != 1 || listed[0].Comment != "deploy" {
		t.Fatal("key not offered to commands", listed, err)
	}
	// A bot without keys keeps the environment it already had.
	if other := api.shellEnv("other")["SSH_AUTH_SOCK"]; other != "" {
		t.Fatal("a bot without keys was given an agent socket", other)
	}
}

func TestWriteSecretGivesAToolTheValue(t *testing.T) {
	const value = "written-secret-value"
	api, _ := startTest(t, &voiceFactory{})
	if err := api.secrets.Save("assistant", secrets.Credential{Name: "signer", Origin: "https://api.example.com", Value: value}); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: store.NewRun("test", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &credentialTool{server: api, action: "write"}
	path := filepath.Join(t.TempDir(), "key.pem")
	args, _ := json.Marshal(map[string]string{"name": "signer", "path": path})
	result, err := tool.Execute(ctx, "call", args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Text(), value) {
		t.Fatal("the value reached the tool result")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("file missing or not private", info, err)
	}
	written, err := os.ReadFile(path)
	if err != nil || string(written) != value {
		t.Fatal("written value differs")
	}
	// A relative path or a missing folder is refused, and a link is never written through.
	relative, _ := json.Marshal(map[string]string{"name": "signer", "path": "key.pem"})
	if _, err := tool.Execute(ctx, "call", relative, nil); err == nil {
		t.Fatal("relative path accepted")
	}
	missing, _ := json.Marshal(map[string]string{"name": "signer", "path": filepath.Join(t.TempDir(), "no", "key.pem")})
	if _, err := tool.Execute(ctx, "call", missing, nil); err == nil {
		t.Fatal("missing folder accepted")
	}
	link := filepath.Join(t.TempDir(), "link.pem")
	target := filepath.Join(t.TempDir(), "target.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	linked, _ := json.Marshal(map[string]string{"name": "signer", "path": link})
	if _, err := tool.Execute(ctx, "call", linked, nil); err == nil {
		t.Fatal("wrote through a symlink")
	}
}

// TestSSHKeyWithPassphrase covers a key that cannot sign through the socket but must still
// be usable, so it is accepted and offered as a file instead.
func TestSSHKeyWithPassphrase(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "locked", []byte("correct horse battery"))
	if err != nil {
		t.Fatal(err)
	}
	api, _ := startTest(t, &voiceFactory{})
	card := requestCard(t, api, map[string]any{"name": "locked", "kind": "ssh", "label": "Locked key", "origin": "github.com"})
	body, _ := json.Marshal(map[string]string{"value": string(pem.EncodeToMemory(block))})
	if recorder := answerCredentialCard(t, api, card, string(body)); recorder.Code != 200 {
		t.Fatal("passphrase-protected key refused", recorder.Code, recorder.Body.String())
	}
	if keys := api.sshKeys("assistant"); len(keys) != 0 {
		t.Fatal("a key needing a passphrase was offered for signing", len(keys))
	}
	state := &turnState{run: store.NewRun("test", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	path := filepath.Join(t.TempDir(), "id_locked")
	args, _ := json.Marshal(map[string]string{"name": "locked", "path": path})
	if _, err := (&credentialTool{server: api, action: "write"}).Execute(ctx, "call", args, nil); err != nil {
		t.Fatal("locked key could not be written", err)
	}
	if _, err := ssh.ParsePrivateKey(writtenSecret(t, path)); err == nil {
		t.Fatal("the written key lost its passphrase")
	}
}

func writtenSecret(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestWriteSecretWorksOutsideDirectChat covers a group chat or child task, where a command can
// already use references and so must also be able to hand the value to a tool.
func TestWriteSecretWorksOutsideDirectChat(t *testing.T) {
	// A group needs two bots, so this server has one more than the shared test server.
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: &voiceFactory{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), VaultKeys: &vaultKeys{},
		Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}}, {Agent: contract.Agent{ID: "peer", Name: "Peer"}}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { api.Close(); db.Close() })
	if _, err := api.Store.CreateThread(t.Context(), "grp-secret", contract.CreateThreadRequest{AgentIDs: []string{"assistant", "peer"}}); err != nil {
		t.Fatal(err)
	}
	if err := api.secrets.Save("assistant", secrets.Credential{Name: "deploy", Origin: "https://api.example.com", Value: "group-value"}); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: store.NewRun("test", "grp-secret", "assistant", "message"), work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	path := filepath.Join(t.TempDir(), "key")
	args, _ := json.Marshal(map[string]string{"name": "deploy", "path": path})
	if _, err := (&credentialTool{server: api, action: "write"}).Execute(ctx, "call", args, nil); err != nil {
		t.Fatal("using a saved secret was blocked outside direct chat", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "group-value" {
		t.Fatal("secret not written", err)
	}
	// Asking for a new secret works the same way, with its card in this chat.
	request, _ := json.Marshal(map[string]string{"name": "other", "label": "Other", "origin": "https://api.example.com"})
	if _, err := (&credentialTool{server: api, action: "request"}).Execute(ctx, "call", request, nil); err != nil {
		t.Fatal("asking for a secret was blocked outside direct chat", err)
	}
	var cards int
	if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM messages WHERE thread_id='grp-secret' AND data LIKE '%"credential"%'`).Scan(&cards); err != nil || cards != 1 {
		t.Fatal("the card was not posted in this chat", cards, err)
	}
}

// TestVaultWriteHandsAValueToATool covers a vault field a program has to read itself.
func TestVaultWriteHandsAValueToATool(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	testVault(t, api)
	if err := api.vault.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	item := contract.VaultItem{ID: "server-login", Kind: "login", Title: "Server", URL: "https://example.com",
		Fields: map[string]string{"username": "owner", "password": "vault-write-value"},
		Codes:  []contract.VaultCode{{ID: "code-1", Value: "recovery-1"}}}
	if _, err := api.vault.Save(t.Context(), vaultItem(item)); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: store.NewRun("test", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	path := filepath.Join(t.TempDir(), "id_vault")
	tool := &vaultTool{server: api, action: "write"}
	call := func(field, target string) (string, error) {
		args, _ := json.Marshal(map[string]string{"id": "server-login", "field": field, "path": target})
		result, err := tool.Execute(ctx, "call", args, nil)
		return result.Text(), err
	}
	// The value stays out of the tool result.
	result, err := call("password", path)
	if err != nil || strings.Contains(result, "vault-write-value") {
		t.Fatal("vault value not written or leaked", err, result)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "vault-write-value" {
		t.Fatal("written value differs", err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("file not private", info, err)
	}
	if _, err := call("missing", path); err == nil {
		t.Fatal("an unknown field was written")
	}
	// A recovery code is spent by handing it out.
	code := filepath.Join(t.TempDir(), "code")
	if _, err := call("recovery", code); err != nil {
		t.Fatal("recovery code unavailable", err)
	}
	if _, err := call("recovery", code); err == nil {
		t.Fatal("the same recovery code was handed out twice")
	}
}
