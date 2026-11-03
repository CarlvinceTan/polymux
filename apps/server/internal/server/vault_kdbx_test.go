package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/tobischo/gokeepasslib/v3"
)

func kdbxFixture(t *testing.T, password string) []byte {
	return kdbxFixtureKey(t, password, nil)
}

func kdbxFixtureKey(t *testing.T, password string, key []byte) []byte {
	t.Helper()
	db := gokeepasslib.NewDatabase()
	var credentials *gokeepasslib.DBCredentials
	var err error
	switch {
	case len(key) > 0 && password != "":
		credentials, err = gokeepasslib.NewPasswordAndKeyDataCredentials(password, key)
	case len(key) > 0:
		credentials, err = gokeepasslib.NewKeyDataCredentials(key)
	default:
		credentials = gokeepasslib.NewPasswordCredentials(password)
	}
	if err != nil {
		t.Fatal(err)
	}
	db.Credentials = credentials
	db.Content = gokeepasslib.NewContent()
	db.Content.Meta = gokeepasslib.NewMetaData()
	db.Content.Root = gokeepasslib.NewRootData()
	group := gokeepasslib.NewGroup()
	group.Name = "KDBX fixture"
	entry := gokeepasslib.NewEntry()
	entry.Values = []gokeepasslib.ValueData{
		{Key: "Title", Value: gokeepasslib.V{Content: "KDBX login"}},
		{Key: "UserName", Value: gokeepasslib.V{Content: "kdbx-user"}},
		{Key: "Password", Value: gokeepasslib.V{Content: "kdbx-secret"}},
	}
	group.Entries = []gokeepasslib.Entry{entry}
	db.Content.Root.Groups = []gokeepasslib.Group{group}
	var output bytes.Buffer
	if err := gokeepasslib.NewEncoder(&output).Encode(db); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func importCard(t *testing.T, s *Server, id string, card contract.VaultCard) {
	t.Helper()
	agent := "assistant"
	card.Status = "pending"
	_, err := s.Store.PostMessage(t.Context(), contract.Message{
		ID: id, ThreadID: agent, Role: "assistant", Status: "done", AgentID: &agent,
		Parts: []contract.Part{{Kind: contract.PartKindVault, Vault: &card}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func callImport(t *testing.T, s *Server, id string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/vault/cards/"+id+"/import", body)
	request.Header.Set("Content-Type", contentType)
	request.SetPathValue("id", id)
	response := httptest.NewRecorder()
	s.importVaultCard(response, request)
	return response
}

func TestKDBXImportCards(t *testing.T) {
	s, _ := startTest(t, nil)
	if err := s.vault.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	data := kdbxFixture(t, "kdbx-password")
	path := filepath.Join(t.TempDir(), "vault.kdbx")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	importCard(t, s, "kdbx-path", contract.VaultCard{Action: "import", Title: "KDBX", Path: &path})
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("password", "kdbx-password"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	response := callImport(t, s, "kdbx-path", body, writer.FormDataContentType())
	if response.Code != 200 {
		t.Fatalf("path import: %d %s", response.Code, response.Body.String())
	}
	items, err := s.vault.List(t.Context())
	if err != nil || len(items) != 1 || items[0].Title != "KDBX login" {
		t.Fatal("path KDBX entry was not imported", items, err)
	}

	uploadBody := new(bytes.Buffer)
	upload := multipart.NewWriter(uploadBody)
	if err := upload.WriteField("password", "kdbx-password"); err != nil {
		t.Fatal(err)
	}
	part, err := upload.CreateFormFile("file", "vault.kdbx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := upload.Close(); err != nil {
		t.Fatal(err)
	}
	importCard(t, s, "kdbx-upload", contract.VaultCard{Action: "import", Title: "KDBX"})
	response = callImport(t, s, "kdbx-upload", uploadBody, upload.FormDataContentType())
	if response.Code != 200 {
		t.Fatalf("upload import: %d %s", response.Code, response.Body.String())
	}
	if items, err = s.vault.List(t.Context()); err != nil || len(items) != 1 {
		t.Fatal("duplicate KDBX upload was not skipped", items, err)
	}

	importCard(t, s, "kdbx-wrong", contract.VaultCard{Action: "import", Title: "KDBX"})
	wrongBody := new(bytes.Buffer)
	wrong := multipart.NewWriter(wrongBody)
	if err := wrong.WriteField("password", "wrong-password"); err != nil {
		t.Fatal(err)
	}
	part, _ = wrong.CreateFormFile("file", "vault.kdbx")
	part.Write(data)
	wrong.Close()
	response = callImport(t, s, "kdbx-wrong", wrongBody, wrong.FormDataContentType())
	if response.Code != 400 || strings.Contains(response.Body.String(), "kdbx-password") {
		t.Fatalf("wrong KDBX password: %d %s", response.Code, response.Body.String())
	}
	for _, table := range []string{"messages", "events", "jobs"} {
		column := map[string]string{"messages": "data", "events": "data", "jobs": "request"}[table]
		for _, value := range []string{"kdbx-password", "kdbx-secret", "kdbx-user"} {
			var count int
			if err := s.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE `+column+` LIKE ?`, "%"+value+"%").Scan(&count); err != nil || count != 0 {
				t.Fatalf("KDBX value entered %s: %v", table, err)
			}
		}
	}
	var issue contract.Error
	if err := json.Unmarshal(response.Body.Bytes(), &issue); err != nil || issue.Message == "" {
		t.Fatal("missing import error", err)
	}

	keyed := bytes.Repeat([]byte{9}, 32)
	keyedData := kdbxFixtureKey(t, "kdbx-password", keyed)
	keyedPath := filepath.Join(t.TempDir(), "vault-keyed.kdbx")
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := os.WriteFile(keyedPath, keyedData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyed, 0600); err != nil {
		t.Fatal(err)
	}
	importCard(t, s, "kdbx-key-path", contract.VaultCard{Action: "import", Title: "KDBX", Path: &keyedPath, KeyPath: &keyPath})
	keyedBody := new(bytes.Buffer)
	keyedForm := multipart.NewWriter(keyedBody)
	if err := keyedForm.WriteField("password", "kdbx-password"); err != nil {
		t.Fatal(err)
	}
	keyedForm.Close()
	response = callImport(t, s, "kdbx-key-path", keyedBody, keyedForm.FormDataContentType())
	if response.Code != 200 {
		t.Fatalf("key-path import: %d %s", response.Code, response.Body.String())
	}

	uploadKeyBody := new(bytes.Buffer)
	uploadKey := multipart.NewWriter(uploadKeyBody)
	uploadKey.WriteField("password", "kdbx-password")
	part, _ = uploadKey.CreateFormFile("file", "vault.kdbx")
	part.Write(keyedData)
	part, _ = uploadKey.CreateFormFile("keyfile", "vault.key")
	part.Write(keyed)
	uploadKey.Close()
	importCard(t, s, "kdbx-key-upload", contract.VaultCard{Action: "import", Title: "KDBX"})
	response = callImport(t, s, "kdbx-key-upload", uploadKeyBody, uploadKey.FormDataContentType())
	if response.Code != 200 {
		t.Fatalf("key upload import: %d %s", response.Code, response.Body.String())
	}
}

func TestVaultImportKDBXPathCreatesPrivateCard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.kdbx")
	if err := os.WriteFile(path, kdbxFixture(t, "kdbx-password"), 0600); err != nil {
		t.Fatal(err)
	}
	factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		result, err := botCall(ctx, d, "vault_import", `{"path":`+strconv.Quote(path)+`}`)
		if err != nil {
			return agentruntime.Result{}, err
		}
		if strings.Contains(result.Text(), "kdbx-secret") || strings.Contains(result.Text(), "kdbx-password") {
			t.Error("KDBX path result exposed secrets")
		}
		return agentruntime.Result{Text: "Private KDBX card ready"}, nil
	}}
	s, _ := startTest(t, factory)
	if err := s.vault.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	botSend(t, s, "assistant", "kdbx-path-run", "Import the KDBX file")
	awaitRun(t, s.Store, store.NewRun("kdbx-path-run", "assistant", "assistant", "message").ID, "completed")
	messages, err := s.Store.Messages(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Vault != nil && part.Vault.Action == "import" && part.Vault.Path != nil {
				found = *part.Vault.Path == path
			}
		}
	}
	if !found {
		t.Fatal("KDBX path did not create a private import card")
	}
	if items, err := s.vault.List(t.Context()); err != nil || len(items) != 0 {
		t.Fatal("KDBX path imported without the user's password", items, err)
	}
}
