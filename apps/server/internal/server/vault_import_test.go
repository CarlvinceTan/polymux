package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/packages/vault"
)

func TestVaultSettingsImport(t *testing.T) {
	s := settingsAPI(t)
	testVault(t, s)
	dir := t.TempDir()
	pairs, err := device.Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer pairs.Close()
	deviceToken, err := pairs.Local(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	s.config.Devices = device.New(pairs)
	call := func(body []byte, contentType string, change func(*http.Request), status int) contract.VaultImport {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/vault/import", bytes.NewReader(body))
		request.RemoteAddr = "127.0.0.1:1234"
		request.Header.Set("Authorization", "Bearer "+deviceToken)
		request.Header.Set("X-Polymux-Client", "polymux-kit")
		request.Header.Set("Content-Type", contentType)
		if change != nil {
			change(request)
		}
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("import: got %d, want %d: %s", response.Code, status, response.Body.String())
		}
		if request.Header.Get("Authorization") != "" && response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("import response is cacheable")
		}
		var result contract.VaultImport
		if status == 200 {
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	csv := []byte("name,username,password\nSettings CSV,csv-user,csv-private-password\n")
	call(csv, "application/octet-stream", func(r *http.Request) { r.Header.Del("Authorization") }, 401)
	call(csv, "application/octet-stream", func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.example") }, 403)
	if result := call(csv, "application/octet-stream", nil, 200); result.Added != 1 || result.Skipped != 0 {
		t.Fatal("CSV was not imported", result)
	}
	if result := call(csv, "application/octet-stream", nil, 200); result.Added != 0 || result.Skipped != 1 {
		t.Fatal("duplicate CSV was not skipped", result)
	}
	call([]byte("not a vault file"), "application/octet-stream", nil, 400)
	call(bytes.Repeat([]byte{'x'}, vault.ImportLimit+1), "application/octet-stream", nil, 400)
	call([]byte(`[{"kind":"login","title":"Partial","fields":{"password":"never-store"}},{"kind":"login","title":""}]`),
		"application/json", nil, 400)
	items, err := s.vault.List(t.Context())
	if err != nil || len(items) != 1 {
		t.Fatal("invalid import partially changed the vault", err)
	}
	data, err := os.ReadFile("../../../../e2e/fixtures/vault-items.json")
	if err != nil {
		t.Fatal(err)
	}
	if result := call(data, "application/json", nil, 200); result.Added != 2 {
		t.Fatal("JSON and software passkey were not imported", result)
	}
	key := bytes.Repeat([]byte{9}, 32)
	kdbx := kdbxFixtureKey(t, "settings-kdbx-password", key)
	upload := func(password string, key []byte, path bool) ([]byte, string) {
		t.Helper()
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		if err := writer.WriteField("password", password); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"file": kdbx, "keyfile": key} {
			if len(data) == 0 {
				continue
			}
			part, err := writer.CreateFormFile(name, name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if path {
			if err := writer.WriteField("path", "/arbitrary/server/file.kdbx"); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return body.Bytes(), writer.FormDataContentType()
	}
	body, contentType := upload("wrong-password", key, false)
	call(body, contentType, nil, 400)
	body, contentType = upload("settings-kdbx-password", nil, false)
	call(body, contentType, nil, 400)
	body, contentType = upload("settings-kdbx-password", key, true)
	call(body, contentType, nil, 400)
	call(kdbx, "application/octet-stream", nil, 400)
	body, contentType = upload("settings-kdbx-password", key, false)
	if result := call(body, contentType, nil, 200); result.Added != 1 {
		t.Fatal("encrypted KDBX was not imported", result)
	}
	if result := call(body, contentType, nil, 200); result.Skipped != 1 {
		t.Fatal("duplicate KDBX was not skipped", result)
	}
	items, err = s.vault.List(t.Context())
	if err != nil || len(items) != 4 {
		t.Fatal("imported items are missing", err)
	}
	for _, item := range items {
		if item.Kind != "passkey" {
			continue
		}
		stored, err := s.vault.Get(t.Context(), item.ID)
		if err != nil || !strings.Contains(stored.Fields["privateKey"], "BEGIN PRIVATE KEY") {
			t.Fatal("software passkey cannot be reopened", err)
		}
		stored.Title = "Edited passkey"
		if _, err := s.vault.Save(t.Context(), stored); err != nil {
			t.Fatal("software passkey cannot be edited", err)
		}
	}
	for _, table := range []string{"messages", "events", "jobs"} {
		column := map[string]string{"messages": "data", "events": "data", "jobs": "request"}[table]
		for _, secret := range []string{"csv-private-password", "settings-import-secret", "BEGIN PRIVATE KEY", "settings-kdbx-password", "kdbx-secret"} {
			var count int
			if err := s.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE `+column+` LIKE ?`, "%"+secret+"%").Scan(&count); err != nil || count != 0 {
				t.Fatalf("import entered %s: %v", table, err)
			}
		}
	}
}
