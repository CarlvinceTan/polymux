package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/packages/vault"
	"github.com/MichaelKinsy/PiG/agent"
)

func (s *Server) postVault(ctx context.Context, call string, ref contract.VaultCard) (agent.AgentToolResult, error) {
	state, err := scopedTurn(ctx, s.Store)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	owner := state.run.AgentID
	ref.Status = "pending"
	key := call
	card, err := s.Store.PostMessage(ctx, contract.Message{
		ID: "vault:" + state.run.ID + ":" + key, ThreadID: state.run.ConversationID, Role: "assistant", Status: "done", CreatedAt: store.Now(),
		AgentID: &owner, RunID: &state.run.ID, Parts: []contract.Part{{Kind: contract.PartKindVault, Vault: &ref}},
	})
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	state.mu.Lock()
	state.posts++
	state.mu.Unlock()
	return toolResult(map[string]string{"message_id": card.ID, "status": card.Parts[0].Vault.Status, "action": ref.Action, "item_id": ref.ItemID})
}

func (t *vaultTool) manage(ctx context.Context, call string, raw json.RawMessage) (agent.AgentToolResult, error) {
	empty := agent.AgentToolResult{}
	state, err := scopedTurn(ctx, t.server.Store)
	if err != nil {
		return empty, err
	}
	var input struct {
		Operation string            `json:"operation"`
		ID        string            `json:"id"`
		Title     string            `json:"title"`
		Kind      string            `json:"kind"`
		URL       string            `json:"url"`
		Revision  int64             `json:"revision"`
		Endpoint  string            `json:"endpoint"`
		Path      string            `json:"path"`
		KeyPath   string            `json:"keyPath"`
		Columns   map[string]string `json:"columns"`
	}
	if err := toolArgs(raw, &input); err != nil {
		return empty, err
	}
	if t.action == "import" {
		if len(input.Path) > 4096 || len(input.KeyPath) > 4096 {
			return empty, errors.New("import paths are too long")
		}
		if input.KeyPath != "" && input.Path == "" {
			return empty, errors.New("keyPath is only used with an encrypted KDBX path")
		}
		if input.Path == "" {
			return t.server.postVault(ctx, call, contract.VaultCard{Action: "import", Title: "Import vault entries"})
		}
		path, err := resolveImportPath(ctx, input.Path)
		if err != nil {
			return empty, err
		}
		data, err := readRegular(path, vault.KDBXLimit)
		if err != nil {
			return empty, errors.New("import requires a regular file up to 32 MiB")
		}
		defer clear(data)
		if vault.IsKDBX(data) {
			ref := contract.VaultCard{Action: "import", Title: "Unlock KDBX import", Path: &path}
			if input.KeyPath != "" {
				keyPath, err := resolveImportPath(ctx, input.KeyPath)
				if err != nil {
					return empty, errors.New("could not open the key file")
				}
				key, err := readRegular(keyPath, vault.KeyFileLimit)
				if err != nil {
					return empty, errors.New("could not open the key file")
				}
				clear(key)
				ref.KeyPath = &keyPath
			}
			return t.server.postVault(ctx, call, ref)
		}
		if input.KeyPath != "" {
			return empty, errors.New("keyPath is only used with an encrypted KDBX path")
		}
		items, err := vault.ParseImport(data, input.Columns)
		if err != nil {
			return empty, importToolError(err)
		}
		result, err := t.server.vault.Import(ctx, items, nil)
		if err != nil {
			return empty, errors.New("vault import was not saved")
		}
		return toolResult(result)
	}
	switch input.Operation {
	case "browser":
		err := t.server.vault.SetBrowser(ctx, vault.Browser{Endpoint: input.Endpoint})
		if err != nil {
			return empty, errors.New("use a loopback Chromium debugging address with a port")
		}
		return toolResult(map[string]bool{"configured": true})
	case "delete":
		if input.Revision < 1 {
			return empty, errors.New("use the current revision from vault_list")
		}
		if err := t.server.vault.Delete(ctx, input.ID, input.Revision); err != nil {
			return empty, err
		}
		return toolResult(map[string]bool{"deleted": true})
	case "add":
		if input.Kind == "" {
			input.Kind = "login"
		}
		kinds := []string{"login", "api", "card", "totp", "note", "recovery", "wifi", "passkey"}
		if !slices.Contains(kinds, input.Kind) || strings.TrimSpace(input.Title) == "" || len(input.Title) > 256 {
			return empty, errors.New("provide an entry type and a short title")
		}
		if input.URL != "" {
			if _, err := vault.Origin(input.URL); err != nil {
				return empty, err
			}
		}
		digest := sha256.Sum256([]byte(state.run.ID + ":" + call))
		return t.server.postVault(ctx, call, contract.VaultCard{Action: "edit", ItemID: hex.EncodeToString(digest[:]), Title: input.Title, Kind: input.Kind, URL: input.URL})
	case "edit":
		item, err := t.server.vault.Get(ctx, input.ID)
		if err != nil {
			return empty, errors.New("vault entry unavailable")
		}
		return t.server.postVault(ctx, call, contract.VaultCard{Action: input.Operation, ItemID: item.ID, Title: item.Title, Kind: item.Kind})
	default:
		return empty, errors.New("choose add, edit, delete, or browser")
	}
}

func (s *Server) saveVaultCard(w http.ResponseWriter, r *http.Request) {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	card, err := s.Store.VaultCard(r.Context(), r.PathValue("id"))
	if err != nil || card.Parts[0].Vault.Action != "edit" {
		fail(w, 409, "vault_card", "This private card is unavailable.")
		return
	}
	ref := card.Parts[0].Vault
	var item contract.VaultItem
	if !decode(w, r, &item) {
		return
	}
	if item.ID != ref.ItemID || item.Kind != ref.Kind {
		vaultError(w, vault.ErrInvalid)
		return
	}
	if ref.Status == "done" {
		stored, err := s.vault.Get(r.Context(), ref.ItemID)
		if !vaultError(w, err) {
			write(w, 200, wireItem(stored))
		}
		return
	}
	saved, err := s.vault.EditWith(r.Context(), vaultItem(item), item.Removed, s.finishVault(r.Context(), card.ID))
	if !vaultError(w, err) {
		write(w, 200, wireItem(saved))
	}
}

func (s *Server) importVaultCard(w http.ResponseWriter, r *http.Request) {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	card, err := s.Store.VaultCard(r.Context(), r.PathValue("id"))
	if err != nil || card.Parts[0].Vault.Action != "import" {
		fail(w, 409, "vault_card", "This private card is unavailable.")
		return
	}
	ref := card.Parts[0].Vault
	if ref.Status == "done" {
		write(w, 200, contract.VaultImport{})
		return
	}
	items, err := s.importItems(r, *ref)
	if err != nil {
		importFailure(w, err)
		return
	}
	result, err := s.vault.Import(r.Context(), items, s.finishVault(r.Context(), card.ID))
	if !vaultError(w, err) {
		write(w, 200, result)
	}
}

// finishVault acknowledges a private card once the vault has saved its result. FinishVault
// ignores a card that is already done, so a repeat is harmless.
func (s *Server) finishVault(ctx context.Context, id string) func() error {
	return func() error {
		tx, err := s.Store.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := s.Store.FinishVault(ctx, tx, id); err != nil {
			return err
		}
		return tx.Commit()
	}
}
