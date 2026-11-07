package server

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/secrets"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/apps/server/internal/untrusted"
	"github.com/CarlvinceTan/polymux/packages/vault"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"golang.org/x/crypto/ssh"
	sshkey "golang.org/x/crypto/ssh/agent"
)

func outputCWD(d agentruntime.Definition, dir string) string {
	if d.Worktree != "" {
		return d.Worktree
	}
	return agentruntime.AgentDir(dir, d.Agent.ID)
}

func credentialOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return "", errors.New("provide an HTTPS origin without credentials or query")
	}
	local := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		local = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", errors.New("credentials require HTTPS; local test services may use HTTP")
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}

func (s *Server) credential(ctx context.Context, owner, name, origin string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, err := s.secrets.Get(owner, name)
	if err != nil {
		return "", fmt.Errorf("credential %s is unavailable; use request_secret for this provider", name)
	}
	if value.IsSSH() {
		return "", errors.New("credential is an SSH key; commands reach it through SSH_AUTH_SOCK")
	}
	want, err := credentialOrigin(origin)
	if err != nil || !value.Allows(want) {
		return "", errors.New("credential is bound to another origin")
	}
	return value.Value, nil
}

// secretRef is a black-box reference the agent can place anywhere: {{secret:name}} or {{vault:id:field}}.
var secretRef = regexp.MustCompile(`\{\{(secret|vault):([A-Za-z0-9_-]{1,128})(?::([A-Za-z]{1,64}))?\}\}`)

// sshHost is a hostname an SSH key belongs to.
var sshHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)

// filler swaps references for values; each value only ever goes to the origin it is bound to.
type filler struct {
	server *Server
	ctx    context.Context
	owner  string
	target string
	values map[string]string
}

func newFiller(ctx context.Context, s *Server, owner, target string) *filler {
	return &filler{server: s, ctx: ctx, owner: owner, target: target, values: map[string]string{}}
}

// fill replaces every reference in text, escaping values for where they land.
func (f *filler) fill(text string, escape func(string) string) (string, error) {
	var failure error
	text = secretRef.ReplaceAllStringFunc(text, func(ref string) string {
		value, err := f.value(ref)
		if err != nil {
			failure = cmp.Or(failure, err)
			return ref
		}
		if escape != nil {
			return escape(value)
		}
		return value
	})
	return text, failure
}

func (f *filler) value(ref string) (string, error) {
	if value, ok := f.values[ref]; ok {
		return value, nil
	}
	m := secretRef.FindStringSubmatch(ref)
	var value string
	switch {
	case m[1] == "secret" && m[3] == "":
		u, err := url.Parse(f.target)
		if err != nil {
			return "", errors.New("invalid secret destination")
		}
		if value, err = f.server.credential(f.ctx, f.owner, m[2], u.Scheme+"://"+u.Host); err != nil {
			return "", err
		}
	case m[1] == "vault" && m[3] != "":
		origin, err := vault.Origin(f.target)
		if err != nil {
			return "", errors.New("vault values need an HTTPS destination")
		}
		values, err := f.server.vault.Values(f.ctx, m[2], origin, []string{m[3]})
		if err != nil {
			return "", fmt.Errorf("vault reference %s: %w", ref, err)
		}
		value = values[m[3]]
	default:
		return "", fmt.Errorf("%s is not a reference; use {{secret:name}} or {{vault:id:field}}", ref)
	}
	f.values[ref] = value
	return value, nil
}

// secrets lists the values filled so far, for redaction.
func (f *filler) secrets() []string {
	values := make([]string, 0, len(f.values))
	for _, value := range f.values {
		values = append(values, value)
	}
	return values
}

// sshKeys returns a bot's saved private keys for its agent socket. A key that no longer
// parses is skipped, so one bad save cannot break the bot's other keys.
func (s *Server) sshKeys(owner string) []sshkey.AddedKey {
	keys := []sshkey.AddedKey{}
	for _, credential := range s.secrets.Values(owner, secrets.KindSSH) {
		raw, err := ssh.ParseRawPrivateKey([]byte(credential.Value))
		if err != nil {
			continue
		}
		keys = append(keys, sshkey.AddedKey{PrivateKey: raw, Comment: credential.Name})
	}
	return keys
}

// shellRuntime runs agent commands with an environment the server chooses.
type shellRuntime interface {
	SetShellEnv(func(agent string) map[string]string)
}

// shellEnv is what a bot's commands and local MCP servers run with: its proxy token and
// the socket holding its SSH keys.
func (s *Server) shellEnv(owner string) map[string]string {
	values := map[string]string{}
	if s.proxy != nil {
		for name, value := range s.proxy.Env(owner) {
			values[name] = value
		}
	}
	if s.ssh != nil {
		for name, value := range s.ssh.Env(owner) {
			values[name] = value
		}
	}
	return values
}

// fillSecrets fills references for the command proxy exactly as http_request does.
func (s *Server) fillSecrets(ctx context.Context, owner, target, text string, escape func(string) string) (string, []string) {
	f := newFiller(ctx, s, owner, target)
	filled, _ := f.fill(text, escape)
	return filled, f.secrets()
}

// boundSecrets reports whether owner has a saved secret or vault entry for origin.
func (s *Server) boundSecrets(ctx context.Context, owner, origin string) bool {
	for _, credential := range s.secrets.List(owner) {
		if credential.Allows(origin) {
			return true
		}
	}
	items, err := s.vault.List(ctx)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.Origin == origin {
			return true
		}
	}
	return false
}

type credentialTool struct {
	server *Server
	action string
}

func (t *credentialTool) Name() string {
	if t.action == "list" {
		return "list_secrets"
	}
	return t.action + "_secret"
}
func (*credentialTool) Label() string                          { return "Manage credential" }
func (*credentialTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*credentialTool) ToolClass() string                      { return "reversible" }
func (t *credentialTool) Schema() ai.ToolSchema {
	properties := map[string]any{}
	required := []string{}
	description := "List saved secrets: names, kinds, bound origins and usual header. Values never enter the model context. Reference an API credential as {{secret:name}} in http_request; an SSH key is already loaded in SSH_AUTH_SOCK."
	if t.action != "list" {
		properties["name"] = map[string]any{"type": "string", "maxLength": 64}
		required = append(required, "name")
	}
	if t.action == "request" {
		description = "Ask the user for a named secret, such as an API key or token, in a masked native card. Values are encrypted on the server, never saved in chat or returned to you, and only sent to a bound origin. Use it in http_request as {{secret:name}} anywhere (URL, headers, body, basic auth), in MCP headers/env, or literally in commands (the proxy fills it for the bound origin). credential=name adds it in its usual header. Give origins for a tool that spans hosts, e.g. gh needs https://api.github.com and https://github.com. ttlMinutes sets an expiry; ephemeral keeps it in memory only, never written to disk — use both for short-lived or one-task secrets. kind=ssh asks for a private key instead: give its host as origin, and the user's commands sign with it through SSH_AUTH_SOCK, so ssh, git over SSH, scp, rsync and gh over SSH work without the key being readable. Existing secrets are reused unless replace=true. The user's save starts a fresh turn."
		for _, name := range []string{"label", "origin", "header", "scheme"} {
			properties[name] = map[string]any{"type": "string", "maxLength": 2048}
		}
		properties["kind"] = map[string]any{"type": "string", "enum": []string{secrets.KindAPI, secrets.KindSSH}, "description": "api for an HTTP credential; ssh for a private key used through SSH_AUTH_SOCK"}
		properties["origins"] = map[string]any{"type": "array", "maxItems": 16, "items": map[string]any{"type": "string", "maxLength": 2048}, "description": "Extra bound origins this one secret may be used at"}
		properties["ttlMinutes"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 525600, "description": "Minutes until the secret expires; omit to keep it until forgotten"}
		properties["ephemeral"] = map[string]any{"type": "boolean", "description": "Keep in memory only; never written to disk and lost on restart"}
		properties["replace"] = map[string]any{"type": "boolean"}
		required = append(required, "label", "origin")
	} else if t.action == "forget" {
		description = "Delete a saved named API credential or SSH key for this bot."
	} else if t.action == "write" {
		description = "Write a saved secret's value to a file for a tool that cannot use references, such as ssh -i, a key file, .netrc, PGPASSFILE or a signer that needs the key itself. Everything else should use {{secret:name}} instead. The file is created owner-only and the value never enters the tool result, but this bot's commands can read the file, so delete it when the task is done. An SSH key that was saved with a passphrase is only usable this way. Use an absolute path."
		properties["path"] = map[string]any{"type": "string", "maxLength": 4096}
		required = append(required, "path")
	}
	return ai.ToolSchema{Name: t.Name(), Description: description, Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}}
}

func (t *credentialTool) Execute(ctx context.Context, call string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, err := scopedTurn(ctx, t.server.Store)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	var input struct {
		Name       string   `json:"name"`
		Label      string   `json:"label"`
		Kind       string   `json:"kind"`
		Origin     string   `json:"origin"`
		Origins    []string `json:"origins"`
		Header     string   `json:"header"`
		Scheme     string   `json:"scheme"`
		TTLMinutes int64    `json:"ttlMinutes"`
		Ephemeral  bool     `json:"ephemeral"`
		Replace    bool     `json:"replace"`
		Path       string   `json:"path"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	owner := state.run.AgentID
	if t.action == "list" {
		return toolResult(t.server.secrets.List(owner))
	}
	if !secrets.ValidName(input.Name) {
		return agent.AgentToolResult{}, errors.New("use a lowercase credential name with letters, digits, hyphens or underscores")
	}
	if t.action == "forget" {
		if err := t.server.secrets.Delete(owner, input.Name); err != nil {
			return agent.AgentToolResult{}, err
		}
		return toolResult(map[string]bool{"deleted": true})
	}
	if t.action == "write" {
		return t.write(owner, input.Name, input.Path)
	}
	kind := cmp.Or(input.Kind, secrets.KindAPI)
	if kind != secrets.KindAPI && kind != secrets.KindSSH {
		return agent.AgentToolResult{}, errors.New("choose kind api or ssh")
	}
	if strings.TrimSpace(input.Label) == "" || len(input.Label) > 256 {
		return agent.AgentToolResult{}, errors.New("provide a short credential label")
	}
	if kind == secrets.KindSSH {
		if len(input.Origins) > 0 {
			return agent.AgentToolResult{}, errors.New("an SSH key has one host; it signs for that host through SSH_AUTH_SOCK")
		}
		if input.Origin = strings.ToLower(strings.TrimSpace(input.Origin)); !sshHost.MatchString(input.Origin) {
			return agent.AgentToolResult{}, errors.New("give the SSH host the key is for, such as github.com")
		}
		input.Header, input.Scheme = "", ""
	}
	origin, origins := input.Origin, []string{}
	if kind == secrets.KindAPI {
		var err error
		if origin, err = credentialOrigin(input.Origin); err != nil {
			return agent.AgentToolResult{}, err
		}
		for _, extra := range input.Origins {
			bound, err := credentialOrigin(extra)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			if bound != origin && !slices.Contains(origins, bound) {
				origins = append(origins, bound)
			}
		}
		if len(origins) > 16 {
			return agent.AgentToolResult{}, errors.New("too many bound origins")
		}
		if input.Header == "" {
			input.Header = "Authorization"
		}
		if input.Scheme == "" && strings.EqualFold(input.Header, "Authorization") {
			input.Scheme = "Bearer"
		}
		if !validHeader(input.Header) || len(input.Scheme) > 32 || strings.ContainsAny(input.Scheme, "\r\n\x00") {
			return agent.AgentToolResult{}, errors.New("invalid credential header")
		}
	}
	if !input.Replace {
		existing, err := t.server.secrets.Get(owner, input.Name)
		if err == nil {
			if existing.Origin != origin || existing.Kind != kind {
				return agent.AgentToolResult{}, errors.New("existing credential has another origin or kind; choose a different name or request replacement")
			}
			return toolResult(map[string]any{"name": input.Name, "available": true, "origin": origin})
		}
		if !errors.Is(err, secrets.ErrMissing) {
			return agent.AgentToolResult{}, errors.New("saved credential could not be read; request explicit replacement to recover")
		}
	}
	ephemeral := input.Ephemeral
	request := contract.CredentialRequest{Name: input.Name, Kind: kind, Label: input.Label, Origin: origin, Origins: origins, Header: input.Header, Scheme: input.Scheme, Ephemeral: &ephemeral, Status: contract.CredentialRequestStatusPending}
	if input.TTLMinutes > 0 {
		expiresAt := time.Now().Add(time.Duration(input.TTLMinutes) * time.Minute).UnixMilli()
		request.ExpiresAt = &expiresAt
	}
	card, err := t.server.Store.PostMessage(ctx, contract.Message{ID: "credential:" + state.run.ID + ":" + call, ThreadID: state.run.ConversationID, Role: contract.MessageRoleAssistant, Status: contract.MessageStatusDone, CreatedAt: store.Now(), AgentID: &owner, RunID: &state.run.ID, Parts: []contract.Part{{Kind: contract.PartKindCredential, Credential: &request}}})
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	state.mu.Lock()
	state.posts++
	state.mu.Unlock()
	status := card.Parts[0].Credential.Status
	if status == "pending" {
		status = "awaiting_user"
	}
	return toolResult(map[string]any{"message_id": card.ID, "status": status, "name": input.Name})
}

// writeFile puts a value where a tool that cannot use references can read it. The staged
// file gets its mode before the rename, so the value is never briefly readable.
func writeFile(path, value string) error {
	if !filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsRune(path, '\x00') {
		return errors.New("use an absolute path")
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to write through a symlink")
	}
	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return errors.New("the folder does not exist; create it first")
	}
	staged := filepath.Join(dir, ".polymux-secret-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := os.WriteFile(staged, []byte(value), 0600); err != nil {
		return errors.New("could not write the file")
	}
	if err := os.Rename(staged, path); err != nil {
		_ = os.Remove(staged)
		return errors.New("could not write the file")
	}
	return nil
}

// write puts a secret where a tool that cannot use references can read it, and never
// returns the value itself.
func (t *credentialTool) write(owner, name, path string) (agent.AgentToolResult, error) {
	if !secrets.ValidName(name) {
		return agent.AgentToolResult{}, errors.New("use a saved secret name and an absolute path")
	}
	credential, err := t.server.secrets.Get(owner, name)
	if err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("secret %s is unavailable", name)
	}
	if err := writeFile(path, credential.Value); err != nil {
		return agent.AgentToolResult{}, err
	}
	return toolResult(map[string]any{"path": path, "written": true})
}

// answerValue returns the entered value, or an empty string when skipped.
func answerValue(a contract.CredentialAnswer) *string {
	if a.Value != nil {
		return a.Value
	}
	empty := ""
	return &empty
}

func validHeader(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return !strings.EqualFold(name, "Host") && !strings.EqualFold(name, "Content-Length")
}

func (s *Server) answerCredential(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	thread, ok := s.thread(w, r)
	if !ok {
		return
	}
	var answer contract.CredentialAnswer
	if !decode(w, r, &answer) {
		return
	}
	if answer.Skip && answer.Value != nil && *answer.Value != "" {
		fail(w, 400, "credential", "Enter a credential or skip this request.")
		return
	}
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	card, err := s.Store.CredentialCard(r.Context(), thread.ID, r.PathValue("messageID"))
	if err != nil {
		fail(w, 409, "credential", "This credential request is unavailable.")
		return
	}
	request := card.Parts[0].Credential
	if request.Status != contract.CredentialRequestStatusPending {
		write(w, 200, card)
		return
	}
	member, err := s.Store.Member(r.Context(), *card.AgentID, thread.ID)
	if err != nil || !member {
		fail(w, 409, "credential", "This bot is no longer available.")
		return
	}
	kind := cmp.Or(request.Kind, secrets.KindAPI)
	// A private key is multiline; every other credential is one line, so it cannot break a header.
	value := *answerValue(answer)
	if !answer.Skip {
		if strings.TrimSpace(value) == "" || len(value) > 16384 || strings.ContainsRune(value, '\x00') || (kind != secrets.KindSSH && strings.ContainsAny(value, "\r\n")) {
			fail(w, 400, "credential", "Enter a credential or skip this request.")
			return
		}
		if kind == secrets.KindSSH {
			// A key with a passphrase cannot sign through the socket; it is still usable as a
			// file, so it is accepted and the agent gets the passphrase separately.
			if _, err := ssh.ParseRawPrivateKey([]byte(value)); err != nil {
				var locked *ssh.PassphraseMissingError
				if !errors.As(err, &locked) {
					fail(w, 400, "credential", "Paste an SSH private key in OpenSSH or PEM format.")
					return
				}
			}
		}
	}
	credential := secrets.Credential{Name: request.Name, Kind: kind, Origin: request.Origin, Origins: request.Origins, Header: request.Header, Scheme: request.Scheme, Value: value}
	if request.ExpiresAt != nil {
		credential.ExpiresAt = *request.ExpiresAt
	}
	credential.Ephemeral = request.Ephemeral != nil && *request.Ephemeral
	var sealed []byte
	if !answer.Skip && !credential.Ephemeral {
		if sealed, err = s.secrets.Seal(*card.AgentID, credential); err != nil {
			fail(w, 500, "credential", "Could not save the credential.")
			return
		}
	}
	result, err := s.Store.CompleteCredential(r.Context(), thread.ID, card.ID, answer.Skip, sealed)
	if err == nil && !answer.Skip && credential.Ephemeral {
		// The card is committed; keep the memory-only secret now, so a failed commit leaves nothing.
		if saveErr := s.secrets.Save(*card.AgentID, credential); saveErr != nil {
			fail(w, 500, "credential", "Could not save the credential.")
			return
		}
	}
	s.respond(w, result, err)
}

type httpTool struct{ server *Server }

func (*httpTool) Name() string                           { return "http_request" }
func (*httpTool) Label() string                          { return "Request API" }
func (*httpTool) ToolClass() string                      { return "consequential" }
func (*httpTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*httpTool) Schema() ai.ToolSchema {
	text := func(description string, limit int) map[string]any {
		return map[string]any{"type": "string", "maxLength": limit, "description": description}
	}
	return ai.ToolSchema{Name: "http_request", Description: "Make an HTTP API request. Saved secrets are black boxes: write {{secret:name}} (list_secrets) or {{vault:id:field}} (vault_list) anywhere in the URL, headers, body or basic auth, and the value is filled in only when the request goes to the origin it is bound to. You never see the value; responses are bounded and filled values are redacted. credential adds a saved secret in its usual header. Redirects are not followed.", Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"url":        text("May contain secret references in its path or query", 8192),
		"method":     map[string]any{"type": "string", "enum": []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"}},
		"headers":    map[string]any{"type": "object", "maxProperties": 32, "additionalProperties": text("Header value; may contain secret references", 8192)},
		"body":       text("May contain secret references", 65536),
		"basic":      text("Basic auth as username:password; either part may be a secret reference", 4096),
		"credential": map[string]any{"type": "string"},
	}, "required": []string{"url"}}}
}
func (t *httpTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, err := scopedTurn(ctx, t.server.Store)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	var in struct {
		URL        string            `json:"url"`
		Method     string            `json:"method"`
		Headers    map[string]string `json:"headers"`
		Body       string            `json:"body"`
		Basic      string            `json:"basic"`
		Credential string            `json:"credential"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return agent.AgentToolResult{}, err
	}
	u, err := url.Parse(in.URL)
	if err != nil || u.Host == "" || strings.Contains(u.Host, "{{") || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || len(in.URL) > 8192 || len(in.Body) > 65536 || len(in.Headers) > 32 || len(in.Basic) > 4096 {
		return agent.AgentToolResult{}, errors.New("invalid API request")
	}
	if in.Method == "" {
		in.Method = "GET"
	}
	if !strings.Contains("|GET|POST|PUT|PATCH|DELETE|HEAD|", "|"+in.Method+"|") {
		return agent.AgentToolResult{}, errors.New("unsupported HTTP method")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	f := newFiller(ctx, t.server, state.run.AgentID, in.URL)
	path, query, _ := strings.Cut(in.URL, "?")
	target, err := f.fill(path, url.PathEscape)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	if query != "" {
		query, err = f.fill(query, url.QueryEscape)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		target += "?" + query
	}
	body, err := f.fill(in.Body, nil)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, in.Method, target, strings.NewReader(body))
	if err != nil {
		return agent.AgentToolResult{}, errors.New("invalid API request")
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range in.Headers {
		if !validHeader(name) || len(value) > 8192 {
			return agent.AgentToolResult{}, errors.New("invalid request header")
		}
		if value, err = f.fill(value, nil); err != nil {
			return agent.AgentToolResult{}, err
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			return agent.AgentToolResult{}, errors.New("invalid request header")
		}
		req.Header.Set(name, value)
	}
	hidden := []string{}
	if in.Basic != "" {
		basic, err := f.fill(in.Basic, nil)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		username, password, _ := strings.Cut(basic, ":")
		req.SetBasicAuth(username, password)
		if basic != in.Basic {
			hidden = append(hidden, base64.StdEncoding.EncodeToString([]byte(basic)))
		}
	}
	if in.Credential != "" {
		secret, err := t.server.credential(ctx, state.run.AgentID, in.Credential, u.Scheme+"://"+u.Host)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		meta, err := t.server.secrets.Get(state.run.AgentID, in.Credential)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		value := secret
		if meta.Scheme != "" {
			value = meta.Scheme + " " + value
		}
		req.Header.Set(meta.Header, value)
		hidden = append(hidden, secret)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return agent.AgentToolResult{}, errors.New("API request failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil {
		return agent.AgentToolResult{}, errors.New("could not read API response")
	}
	text := untrusted.Wrap(u.Host, redactBody(string(data[:min(len(data), 65536)]), append(hidden, f.secrets()...)))
	result, err := toolResult(map[string]any{"status": response.StatusCode, "body": text, "truncated": len(data) > 65536})
	result.IsError = response.StatusCode < 200 || response.StatusCode >= 300
	return result, err
}

// redactBody hides each secret, including its URL- and JSON-escaped forms.
func redactBody(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = redactOne(text, secret)
		}
	}
	return text
}

func redactOne(text, secret string) string {
	redact := func(value string) string {
		value = strings.ReplaceAll(value, secret, "[redacted]")
		return strings.ReplaceAll(value, url.QueryEscape(secret), "[redacted]")
	}
	var body any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if decoder.Decode(&body) == nil && decoder.Decode(new(any)) == io.EOF {
		var walk func(any) any
		walk = func(value any) any {
			switch value := value.(type) {
			case string:
				return redact(value)
			case []any:
				for i := range value {
					value[i] = walk(value[i])
				}
			case map[string]any:
				clean := make(map[string]any, len(value))
				for key, item := range value {
					clean[redact(key)] = walk(item)
				}
				return clean
			}
			return value
		}
		data, _ := json.Marshal(walk(body))
		text = string(data)
	}
	encoded, _ := json.Marshal(secret)
	pattern := string(encoded[1 : len(encoded)-1])
	text = strings.ReplaceAll(text, strings.ReplaceAll(pattern, "/", `\/`), "[redacted]")
	text = strings.ReplaceAll(text, pattern, "[redacted]")
	return redact(text)
}
