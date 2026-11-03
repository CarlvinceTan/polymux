package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/packages/vault"
)

func TestVaultPrivateFlow(t *testing.T) {
	const secret = "private-imported-password"
	path := filepath.Join(t.TempDir(), "import.csv")
	if err := os.WriteFile(path, []byte("name,url,username,password\nImported,https://example.com,private-user,"+secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, m contract.Message) (agentruntime.Result, error) {
		call := func(name string, input any) (string, error) {
			raw, _ := json.Marshal(input)
			result, err := botCall(ctx, d, name, string(raw))
			if strings.Contains(result.Text(), secret) || strings.Contains(result.Text(), "private-user") {
				t.Error("vault tool leaked imported fields")
			}
			return result.Text(), err
		}
		switch m.ID {
		case "import-run":
			if _, err := call("vault_import", map[string]string{"path": path}); err != nil {
				return agentruntime.Result{}, err
			}
			text, err := call("vault_list", map[string]string{})
			if err != nil {
				return agentruntime.Result{}, err
			}
			var items []contract.VaultSummary
			if err := json.Unmarshal([]byte(text), &items); err != nil || len(items) != 1 {
				return agentruntime.Result{}, errors.New("import receipt missing")
			}
			if _, err := call("vault_manage", map[string]string{"operation": "edit", "id": items[0].ID}); err != nil {
				return agentruntime.Result{}, err
			}
		case "add-run":
			if _, err := call("vault_manage", map[string]string{"operation": "add", "kind": "login", "title": "Manual", "url": "https://example.com"}); err != nil {
				return agentruntime.Result{}, err
			}
		}
		data, _ := json.Marshal(m)
		if strings.Contains(string(data), secret) || strings.Contains(string(data), "private-manual-password") {
			t.Error("private fields entered a continuation")
		}
		return agentruntime.Result{Text: "Private operation ready"}, nil
	}}
	s, _ := startTest(t, factory)
	testVault(t, s)
	if err := s.vault.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"import-run", "add-run"} {
		botSend(t, s, "assistant", id, "Manage my vault")
		awaitRun(t, s.Store, store.NewRun(id, "assistant", "assistant", "message").ID, "completed")
	}
	messages, err := s.Store.Messages(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var importedCard, edit contract.Message
	for _, message := range messages {
		if len(message.Parts) != 1 || message.Parts[0].Vault == nil {
			continue
		}
		switch message.Parts[0].Vault.Action {
		case "edit":
			if message.Parts[0].Vault.Title == "Imported" {
				importedCard = message
			} else {
				edit = message
			}
		}
	}
	if importedCard.ID == "" || edit.ID == "" {
		t.Fatal("private cards missing")
	}
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
	call := func(method, path, body string, status int) []byte {
		t.Helper()
		req, _ := http.NewRequest(method, api.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := api.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		for _, value := range []string{secret, "private-user", "private-manual-password", "private-upload-password"} {
			if strings.Contains(string(data), value) {
				t.Fatal("private response returned a saved value")
			}
		}
		if response.StatusCode != status {
			t.Fatalf("private API %s: got %d want %d", path, response.StatusCode, status)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("private response was cacheable")
		}
		return data
	}
	readPath := "/vault/items/" + importedCard.Parts[0].Vault.ItemID
	if strings.Contains(string(call("GET", readPath, "", 200)), secret) {
		t.Fatal("private edit returned a saved value")
	}
	item := contract.VaultItem{ID: edit.Parts[0].Vault.ItemID, Kind: "login", Title: "Manual", URL: "https://example.com", Fields: map[string]string{"password": "private-manual-password"}, Codes: []contract.VaultCode{}}
	data, _ := json.Marshal(item)
	if _, err := s.Store.DB.ExecContext(t.Context(), `CREATE TRIGGER reject_vault BEFORE UPDATE ON messages WHEN NEW.id='`+edit.ID+`' BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	call("POST", "/vault/cards/"+edit.ID+"/save", string(data), 500)
	if _, err := s.vault.Get(t.Context(), item.ID); !errors.Is(err, vault.ErrMissing) {
		t.Fatal("failed acknowledgement left an entry")
	}
	if _, err := s.Store.DB.ExecContext(t.Context(), `DROP TRIGGER reject_vault`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		call("POST", "/vault/cards/"+edit.ID+"/save", string(data), 200)
	}
	var jobs int
	if err := s.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs WHERE id=?`, "vault-answer:"+edit.ID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("private save duplicated continuation", err)
	}
	importCard, err := s.Store.PostMessage(t.Context(), contract.Message{
		ID: "private-upload", ThreadID: "assistant", AgentID: edit.AgentID, Role: "assistant", Status: "done", CreatedAt: store.Now(),
		Parts: []contract.Part{{Kind: contract.PartKindVault, Vault: &contract.VaultCard{Action: "import", Status: "pending", Title: "Import"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	call("POST", "/vault/cards/"+importCard.ID+"/import", "name,password\nUpload,private-upload-password\nmalformed,row,extra", 400)
	var imported contract.VaultImport
	if err := json.Unmarshal(call("POST", "/vault/cards/"+importCard.ID+"/import", "name,password\nUpload,private-upload-password", 200), &imported); err != nil || imported.Added != 1 {
		t.Fatal("private upload failed", err)
	}
	call("POST", "/vault/cards/"+importCard.ID+"/import", "name,password\nUpload,private-upload-password", 200)
	for _, table := range []string{"messages", "events", "jobs"} {
		column := map[string]string{"messages": "data", "events": "data", "jobs": "request"}[table]
		for _, value := range []string{secret, "private-user", "private-manual-password", "private-upload-password"} {
			var count int
			if err := s.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE `+column+` LIKE ?`, "%"+value+"%").Scan(&count); err != nil || count != 0 {
				t.Fatal("private value entered persistence", table, err)
			}
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("import deleted source", err)
	}
}

// TestVaultCardWorksInGroupChat covers a private card in any chat, not only the bot's own thread.
func TestVaultCardWorksInGroupChat(t *testing.T) {
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
	if err := api.vault.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Store.CreateThread(t.Context(), "grp-vault", contract.CreateThreadRequest{AgentIDs: []string{"assistant", "peer"}}); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: store.NewRun("test", "grp-vault", "assistant", "message"), work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	args, _ := json.Marshal(map[string]string{"operation": "add", "kind": "login", "title": "Group login", "url": "https://example.com"})
	result, err := (&vaultTool{server: api, action: "manage"}).Execute(ctx, "call", args, nil)
	if err != nil {
		t.Fatal("private card refused in a group chat", err)
	}
	var posted struct {
		ID string `json:"message_id"`
	}
	if err := json.Unmarshal([]byte(result.Text()), &posted); err != nil || posted.ID == "" {
		t.Fatal("no card posted", result.Text(), err)
	}
	// The card lives in this chat, and answering it resumes the bot here.
	card, err := api.Store.VaultCard(t.Context(), posted.ID)
	if err != nil || card.ThreadID != "grp-vault" {
		t.Fatal("card not usable in the group chat", card.ThreadID, err)
	}
	item := contract.VaultItem{ID: card.Parts[0].Vault.ItemID, Kind: "login", Title: "Group login", URL: "https://example.com",
		Fields: map[string]string{"password": "group-card-password"}, Codes: []contract.VaultCode{}}
	if _, err := api.vault.SaveWith(t.Context(), vaultItem(item), func(tx *sql.Tx) error {
		return api.Store.FinishVault(t.Context(), tx, card.ID)
	}); err != nil {
		t.Fatal(err)
	}
	done, err := api.Store.VaultCard(t.Context(), card.ID)
	if err != nil || done.Parts[0].Vault.Status != "done" {
		t.Fatal("card not finished", err)
	}
	var jobs int
	if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs WHERE id=?`, "vault-answer:"+card.ID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("the bot was not resumed in the group chat", jobs, err)
	}
}
