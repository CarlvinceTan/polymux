package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/secrets"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/apps/server/internal/untrusted"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

func outputCWD(d agentruntime.Definition, dir string) string {
	if d.Worktree != "" {
		return d.Worktree
	}
	return filepath.Join(dir, d.Agent.ID)
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
	want, err := credentialOrigin(origin)
	if err != nil || value.Origin != want {
		return "", errors.New("credential is bound to another origin")
	}
	return value.Value, nil
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
	description := "List saved credential names and their bound origins. Values never enter the model context."
	if t.action != "list" {
		properties["name"] = map[string]any{"type": "string", "maxLength": 64}
		required = append(required, "name")
	}
	if t.action == "request" {
		description = "Ask the user for a named API credential in a masked native card. Values are encrypted on the server, never saved in chat or returned to you. Use http_request or the native provider tool with the credential name. Existing credentials are reused unless replace=true. The user's save starts a fresh turn. Direct-chat only."
		for _, name := range []string{"label", "origin", "header", "scheme"} {
			properties[name] = map[string]any{"type": "string", "maxLength": 2048}
		}
		properties["replace"] = map[string]any{"type": "boolean"}
		required = append(required, "label", "origin")
	} else if t.action == "forget" {
		description = "Delete a saved named API credential for this bot."
	}
	return ai.ToolSchema{Name: t.Name(), Description: description, Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}}
}

func (t *credentialTool) Execute(ctx context.Context, call string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, err := scopedTurn(ctx, t.server.Store)
	if err != nil || state.run.AgentID != state.run.ConversationID {
		return agent.AgentToolResult{}, errors.New("credential management is direct-chat only")
	}
	var input struct {
		Name    string `json:"name"`
		Label   string `json:"label"`
		Origin  string `json:"origin"`
		Header  string `json:"header"`
		Scheme  string `json:"scheme"`
		Replace bool   `json:"replace"`
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
	origin, err := credentialOrigin(input.Origin)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	if strings.TrimSpace(input.Label) == "" || len(input.Label) > 256 {
		return agent.AgentToolResult{}, errors.New("provide a short credential label")
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
	if !input.Replace {
		existing, err := t.server.secrets.Get(owner, input.Name)
		if err == nil {
			if existing.Origin != origin {
				return agent.AgentToolResult{}, errors.New("existing credential has another origin; choose a different name or request replacement")
			}
			return toolResult(map[string]any{"name": input.Name, "available": true, "origin": origin})
		}
		if !errors.Is(err, secrets.ErrMissing) {
			return agent.AgentToolResult{}, errors.New("saved credential could not be read; request explicit replacement to recover")
		}
	}
	request := contract.CredentialRequest{Name: input.Name, Label: input.Label, Origin: origin, Header: input.Header, Scheme: input.Scheme, Status: "pending"}
	card, err := t.server.Store.PostMessage(ctx, contract.Message{ID: "credential:" + state.run.ID + ":" + call, ThreadID: state.run.ConversationID, Role: "assistant", Status: "done", CreatedAt: store.Now(), AgentID: &owner, RunID: &state.run.ID, Parts: []contract.Part{{Kind: "credential", Credential: &request}}})
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
	if (!answer.Skip && (answer.Value == nil || strings.TrimSpace(*answer.Value) == "" || len(*answer.Value) > 16384 || strings.ContainsAny(*answer.Value, "\r\n\x00"))) || (answer.Skip && answer.Value != nil && *answer.Value != "") {
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
	if request.Status != "pending" {
		write(w, 200, card)
		return
	}
	member, err := s.Store.Member(r.Context(), *card.AgentID, thread.ID)
	if err != nil || !member {
		fail(w, 409, "credential", "This bot is no longer available.")
		return
	}
	var sealed []byte
	if !answer.Skip {
		sealed, err = s.secrets.Seal(*card.AgentID, secrets.Credential{Name: request.Name, Origin: request.Origin, Header: request.Header, Scheme: request.Scheme, Value: *answer.Value})
		if err != nil {
			fail(w, 500, "credential", "Could not save the credential.")
			return
		}
	}
	result, err := s.Store.CompleteCredential(r.Context(), thread.ID, card.ID, answer.Skip, sealed)
	s.respond(w, result, err)
}

type httpTool struct{ server *Server }

func (*httpTool) Name() string                           { return "http_request" }
func (*httpTool) Label() string                          { return "Request API" }
func (*httpTool) ToolClass() string                      { return "consequential" }
func (*httpTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*httpTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "http_request", Description: "Make an HTTP API request, optionally using a named origin-bound credential. Never pass credential values in arguments. Redirects are not followed. Responses are bounded and credential values are redacted.", Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"url": map[string]any{"type": "string", "maxLength": 8192}, "method": map[string]any{"type": "string", "enum": []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"}}, "body": map[string]any{"type": "string", "maxLength": 65536}, "credential": map[string]any{"type": "string"}}, "required": []string{"url"}}}
}
func (t *httpTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, err := scopedTurn(ctx, t.server.Store)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	var in struct {
		URL        string `json:"url"`
		Method     string `json:"method"`
		Body       string `json:"body"`
		Credential string `json:"credential"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return agent.AgentToolResult{}, err
	}
	u, err := url.Parse(in.URL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || len(in.URL) > 8192 || len(in.Body) > 65536 {
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
	req, err := http.NewRequestWithContext(ctx, in.Method, in.URL, strings.NewReader(in.Body))
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	secret := ""
	if in.Credential != "" {
		secret, err = t.server.credential(ctx, state.run.AgentID, in.Credential, u.Scheme+"://"+u.Host)
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
	text := untrusted.Wrap(u.Host, redactBody(string(data[:min(len(data), 65536)]), secret))
	result, err := toolResult(map[string]any{"status": response.StatusCode, "body": text, "truncated": len(data) > 65536})
	result.IsError = response.StatusCode < 200 || response.StatusCode >= 300
	return result, err
}

func redactBody(text, secret string) string {
	if secret == "" {
		return text
	}
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
