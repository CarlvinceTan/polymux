package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/secrets"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestCredentialJSONEcho(t *testing.T) {
	const value = `private-"quoted"-\value/part`
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+value {
			http.Error(w, "missing credential", http.StatusUnauthorized)
			return
		}
		data, _ := json.Marshal(map[string]string{"credential": value})
		if r.URL.Path == "/slashes" {
			data = []byte(strings.ReplaceAll(string(data), "/", `\/`))
		}
		_, _ = w.Write(data)
	}))
	defer origin.Close()
	api, _ := startTest(t, &voiceFactory{})
	if err := api.secrets.Save("assistant", secrets.Credential{Name: "service", Origin: origin.URL, Header: "Authorization", Scheme: "Bearer", Value: value}); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: store.NewRun("json-echo", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	for _, path := range []string{"/json", "/slashes"} {
		t.Run(path, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"url": origin.URL + path, "credential": "service"})
			result, err := (&httpTool{server: api}).Execute(ctx, "echo", args, nil)
			if err != nil {
				t.Fatal(err)
			}
			var output struct {
				Body string `json:"body"`
			}
			if err := json.Unmarshal([]byte(result.Text()), &output); err != nil {
				t.Fatal(err)
			}
			var echo map[string]string
			if err := json.Unmarshal([]byte(output.Body), &echo); err == nil && echo["credential"] == value {
				t.Fatal("JSON-escaped credential reached model tool output")
			}
			encoded, _ := json.Marshal(value)
			if strings.Contains(output.Body, value) || strings.Contains(output.Body, string(encoded[1:len(encoded)-1])) {
				t.Fatal("credential representation remains in tool output")
			}
		})
	}
}

func TestCredentialSaveRollback(t *testing.T) {
	for _, previous := range []string{"", "old-private-value"} {
		name := "new"
		if previous != "" {
			name = "replace"
		}
		t.Run(name, func(t *testing.T) {
			api, _ := startTest(t, &voiceFactory{})
			owner := "assistant"
			credential := secrets.Credential{Name: "service", Origin: "https://api.example.com", Header: "Authorization", Scheme: "Bearer", Value: previous}
			if previous != "" {
				if err := api.secrets.Save(owner, credential); err != nil {
					t.Fatal(err)
				}
			}
			request := contract.CredentialRequest{Name: credential.Name, Label: "Service key", Origin: credential.Origin, Header: credential.Header, Scheme: credential.Scheme, Status: "pending"}
			card, err := api.Store.PostMessage(t.Context(), contract.Message{ID: "credential:rollback", ThreadID: owner, Role: "assistant", Status: "done", CreatedAt: store.Now(), AgentID: &owner, Parts: []contract.Part{{Kind: "credential", Credential: &request}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := api.Store.DB.ExecContext(t.Context(), `CREATE TRIGGER reject_credential BEFORE UPDATE ON messages WHEN NEW.id='credential:rollback' BEGIN SELECT RAISE(ABORT,'injected credential completion failure'); END`); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRecorder()
			api.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages/"+card.ID+"/credential", strings.NewReader(`{"value":"new-private-value"}`)))
			if r.Code != http.StatusInternalServerError {
				t.Fatal("completion failure status", r.Code)
			}
			stored, err := api.secrets.Get(owner, credential.Name)
			if previous == "" && !errors.Is(err, secrets.ErrMissing) {
				t.Fatal("failed card completion retained a new credential")
			}
			if previous != "" && (err != nil || stored.Value != previous) {
				t.Fatal("failed card completion replaced the existing credential")
			}
			card, err = api.Store.CredentialCard(t.Context(), owner, card.ID)
			if err != nil || card.Parts[0].Credential.Status != "pending" {
				t.Fatal("failed card completion changed request state", err)
			}
			if _, err := api.Store.DB.ExecContext(t.Context(), `DROP TRIGGER reject_credential`); err != nil {
				t.Fatal(err)
			}
			r = httptest.NewRecorder()
			api.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages/"+card.ID+"/credential", strings.NewReader(`{"skip":true}`)))
			if r.Code != http.StatusOK {
				t.Fatal("skip after failed save", r.Code)
			}
		})
	}
}

func TestCredentialRequestReplay(t *testing.T) {
	for _, status := range []string{"saved", "skipped"} {
		t.Run(status, func(t *testing.T) {
			api, _ := startTest(t, &voiceFactory{})
			state := &turnState{run: store.NewRun("replay", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}}
			ctx := context.WithValue(t.Context(), turnKey{}, state)
			tool := &credentialTool{server: api, action: "request"}
			args := json.RawMessage(`{"name":"service","label":"Service key","origin":"https://api.example.com","replace":true}`)
			result, err := tool.Execute(ctx, "credential-call", args, nil)
			if err != nil {
				t.Fatal(err)
			}
			var receipt struct {
				ID string `json:"message_id"`
			}
			if err := json.Unmarshal([]byte(result.Text()), &receipt); err != nil || receipt.ID == "" {
				t.Fatal("credential card not posted", err)
			}
			body := `{"value":"replay-private-value"}`
			if status == "skipped" {
				body = `{"skip":true}`
			}
			r := httptest.NewRecorder()
			api.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages/"+receipt.ID+"/credential", strings.NewReader(body)))
			if r.Code != http.StatusOK {
				t.Fatal("credential answer failed", r.Code)
			}
			result, err = tool.Execute(ctx, "credential-call", args, nil)
			if err != nil {
				t.Fatal(err)
			}
			var replay map[string]any
			if err := json.Unmarshal([]byte(result.Text()), &replay); err != nil {
				t.Fatal(err)
			}
			if status == "saved" && replay["status"] != "saved" && replay["available"] != true {
				t.Fatal("saved replacement replay waits for another credential answer")
			}
			if status == "skipped" && replay["status"] != "skipped" {
				t.Fatal("skipped request replay waits for another credential answer")
			}
		})
	}
}

func TestCredentialUnreadable(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	if err := api.secrets.Save("assistant", secrets.Credential{Name: "service", Origin: "https://api.example.com", Value: "existing-private-value"}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Store.DB.ExecContext(t.Context(), `UPDATE credentials SET data=? WHERE owner=? AND name=?`, []byte("corrupt"), "assistant", "service"); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: store.NewRun("unreadable", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	_, err := (&credentialTool{server: api, action: "request"}).Execute(ctx, "credential-call", json.RawMessage(`{"name":"service","label":"Service key","origin":"https://api.example.com"}`), nil)
	if err == nil {
		t.Fatal("unreadable credential was treated as absent without replacement authorization")
	}
}
