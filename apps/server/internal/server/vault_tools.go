package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/packages/vault"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type vaultTool struct {
	server *Server
	action string
}

func (t *vaultTool) Name() string                         { return "vault_" + t.action }
func (*vaultTool) Label() string                          { return "Use vault" }
func (*vaultTool) Deferred() bool                         { return true }
func (*vaultTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *vaultTool) ToolClass() string {
	if t.action == "list" {
		return "read"
	}
	return "consequential"
}

func (t *vaultTool) Schema() ai.ToolSchema {
	text := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	properties := map[string]any{}
	required := []string{}
	description := "List vault IDs, labels, origins, field names and revisions; never values. Use a field as {{vault:id:field}} anywhere in http_request (totp gives a current code). Use vault_manage for changes, vault_write to hand one field to a tool that needs the value itself. Manage entries in Settings → Vault or private edit cards; saved values cannot be viewed."
	if t.action != "list" && t.action != "manage" && t.action != "import" {
		properties["id"] = text("Saved vault item ID; never a credential value")
		required = append(required, "id")
	}
	if t.action == "write" {
		description = "Write one field's value to an owner-only file for a tool that needs the value itself, such as a key file, a .netrc, a PGPASSFILE or a signer loading a key. Use vault_request or vault_fill whenever they work instead. The value never enters the tool result, but this bot's commands can read the file, so delete it when the task is done. A recovery code is marked used. Use an absolute path."
		properties["field"] = text("Vault field name, such as password, privateKey, totp, notes, or recovery")
		properties["path"] = text("Absolute path for the file")
		required = append(required, "field", "path")
	}
	switch t.action {
	case "manage":
		description = "Manage vault entries without secret arguments. Add/edit opens a private entry card for the user; never ask for passwords in chat. Delete needs the current revision. Browser configures a loopback Chromium endpoint. A completed private edit resumes the task automatically."
		properties["operation"] = map[string]any{"type": "string", "enum": []string{"add", "edit", "delete", "browser"}}
		for _, field := range []string{"id", "title", "kind", "url", "endpoint"} {
			properties[field] = text("Non-secret entry metadata or browser endpoint")
		}
		properties["revision"] = map[string]any{"type": "integer"}
		required = []string{"operation"}
	case "import":
		description = "Import a CSV, JSON, or encrypted KDBX file directly into the vault; never read it with shell/file tools or pass secrets as arguments. Supply a server path (relative to workspace or absolute), or omit path to offer a private file picker. Encrypted KDBX needs that file’s password and optional key file, entered only in the private card; never ask for them in chat. Supply keyPath only with a KDBX path. CSV supports common name/url/username/password/notes/TOTP columns and common Chrome, Bitwarden, 1Password, LastPass, and KeePass headers; optional columns maps canonical field names to header names. JSON is an array of vault items or an unencrypted Bitwarden JSON export. At most 8 MiB for CSV/JSON, 32 MiB for KDBX, and 1000 entries; atomic, identical reimports skipped; changed rows become new entries, no existing entries overwritten. KDBX attachments, binaries, and history are not imported; custom fields and group paths go to notes. Returns counts only. Source file remains untouched."
		properties["path"] = text("Import file path, not file contents; omit for private upload")
		properties["keyPath"] = text("Optional KDBX key-file path, only with an encrypted KDBX path")
		properties["columns"] = map[string]any{"type": "object", "additionalProperties": text("CSV header name for this canonical field (title, url, username, password, totp, notes, etc.)")}
	case "request":
		description = "Send an HTTP request using vault field references, without receiving their values. The saved website's exact origin is enforced; redirects are refused. Returns HTTP status only, never response bodies, cookies, headers, or tokens. Do not blindly retry: a failed request may have taken effect. Recovery codes are marked used before dispatch."
		properties["url"] = text("HTTPS destination at the item's saved origin")
		properties["method"] = map[string]any{"type": "string", "enum": []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"}}
		properties["auth"] = map[string]any{"type": "string", "enum": []string{"bearer", "basic"}, "description": "Bearer uses token; basic uses username and password"}
		properties["fields"] = map[string]any{"type": "object", "additionalProperties": text("Vault field name, such as password, number, cvc, totp (generated code), notes, or recovery (one unused code)")}
		properties["values"] = map[string]any{"type": "object", "additionalProperties": text("Non-secret request value")}
		properties["form"] = map[string]any{"type": "boolean", "description": "Use form encoding instead of JSON"}
		required = append(required, "url")
	case "fill":
		description = "Fill visible fields on the saved website in a configured Chromium browser without returning credential values. Does not submit the form. Never inspect filled values or request screenshots of visible secrets; continue with the intended submit action. Configure Chromium with vault_manage operation=browser. Existing host tools remain unrestricted. Recovery codes are marked used before filling and are not automatically retried."
		properties["target"] = text("Exact Chromium page target ID when several pages share the saved origin")
		properties["bindings"] = map[string]any{"type": "array", "maxItems": 16, "description": "Optional explicit fields; defaults to login username/password or TOTP", "items": map[string]any{
			"type": "object", "additionalProperties": false, "properties": map[string]any{
				"field":    text("Vault field name; totp generates a code, recovery reserves one unused code"),
				"selector": text("CSS selector for a visible input or textarea on the saved origin"),
			}, "required": []string{"field", "selector"},
		}}
	case "passkey":
		description = "Use an imported software passkey in Chromium without receiving key material. Returns a ready operation ID; then trigger the site's passkey sign-in and poll with operation. Keep existing browser CDP connections open during the ceremony. The authenticator is removed after use or two minutes. This does not register new passkeys or access Apple Passwords/hardware keys."
		properties["target"] = text("Exact Chromium page target ID when needed")
		properties["operation"] = text("Poll an earlier operation instead of starting another; omit id when polling")
		required = []string{}
	}
	return ai.ToolSchema{Name: t.Name(), Description: description, Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": properties, "required": required,
	}}
}

func (t *vaultTool) Execute(ctx context.Context, call string, raw json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	if t.action == "manage" || t.action == "import" {
		return t.manage(ctx, call, raw)
	}
	state, err := scopedTurn(ctx, t.server.Store)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	var result any
	switch t.action {
	case "list":
		var input struct{}
		if err = toolArgs(raw, &input); err == nil {
			result, err = t.server.vault.List(ctx)
		}
	case "request":
		var input vault.Request
		if err = toolArgs(raw, &input); err == nil {
			result, err = t.server.vault.Request(ctx, input)
		}
	case "fill":
		var input struct {
			ID       string          `json:"id"`
			Target   string          `json:"target"`
			Bindings []vault.Binding `json:"bindings"`
		}
		if err = toolArgs(raw, &input); err == nil {
			result, err = t.server.vault.Fill(ctx, input.ID, input.Target, input.Bindings)
		}
	case "write":
		var input struct {
			ID    string `json:"id"`
			Field string `json:"field"`
			Path  string `json:"path"`
		}
		var value string
		if err = toolArgs(raw, &input); err == nil {
			if value, err = t.server.vault.File(ctx, input.ID, input.Field); err == nil {
				if err = writeFile(input.Path, value); err == nil {
					result = map[string]any{"path": input.Path, "written": true}
				}
			}
		}
	case "passkey":
		var input struct {
			ID        string `json:"id"`
			Target    string `json:"target"`
			Operation string `json:"operation"`
		}
		if err = toolArgs(raw, &input); err == nil {
			if input.Operation != "" {
				result, err = t.server.vaultOperation(state.run.AgentID, input.Operation)
			} else {
				result, err = t.server.startPasskey(ctx, state.run.AgentID, input.ID, input.Target)
			}
		}
	}
	if err != nil {
		if errors.Is(err, vault.ErrUnavailable) {
			err = errors.New("vault unavailable; check the server’s secure key storage")
		}
		return agent.AgentToolResult{}, err
	}
	output, err := toolResult(result)
	if receipt, ok := result.(vault.Receipt); ok {
		output.IsError = receipt.Status < 200 || receipt.Status >= 300
	}
	return output, err
}

type vaultJob struct {
	owner   string
	item    string
	state   string
	failure error
	expires time.Time
}

func (s *Server) vaultOperation(owner, id string) (map[string]string, error) {
	s.vaultMu.Lock()
	defer s.vaultMu.Unlock()
	job := s.vaultJobs[id]
	if job == nil || job.owner != owner || time.Now().After(job.expires) {
		return nil, errors.New("vault operation is unavailable")
	}
	// Passkey errors are fixed text, never key material.
	if job.failure != nil {
		return nil, job.failure
	}
	return map[string]string{"operation": id, "status": job.state}, nil
}

func (s *Server) startPasskey(ctx context.Context, owner, item, target string) (map[string]string, error) {
	s.vaultMu.Lock()
	for id, job := range s.vaultJobs {
		if time.Now().After(job.expires) {
			delete(s.vaultJobs, id)
			continue
		}
		if job.item == item && (job.state == "starting" || job.state == "ready") {
			s.vaultMu.Unlock()
			return nil, errors.New("a passkey operation is already active for this entry")
		}
	}
	if len(s.vaultJobs) >= 32 {
		s.vaultMu.Unlock()
		return nil, errors.New("too many recent passkey operations")
	}
	var bytes [16]byte
	_, _ = rand.Read(bytes[:])
	id := hex.EncodeToString(bytes[:])
	job := &vaultJob{owner: owner, item: item, state: "starting", expires: time.Now().Add(5 * time.Minute)}
	s.vaultJobs[id] = job
	s.vaultMu.Unlock()
	ready := make(chan struct{})
	var once sync.Once
	signal := func() { once.Do(func() { close(ready) }) }
	work, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	s.workers.Add(1)
	s.admission.fork()
	go func() {
		defer s.admission.done()
		defer s.workers.Done()
		defer cancel()
		err := s.vault.Passkey(work, item, target, func() {
			s.vaultMu.Lock()
			job.state = "ready"
			s.vaultMu.Unlock()
			signal()
		})
		s.vaultMu.Lock()
		job.state = "completed"
		if err != nil {
			job.state, job.failure = "failed", err
		}
		s.vaultMu.Unlock()
		signal()
	}()
	select {
	case <-ready:
		return s.vaultOperation(owner, id)
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
}
