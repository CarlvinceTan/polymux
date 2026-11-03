package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestBotPluginWaitsForAPerson(t *testing.T) {
	folder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(folder, "ui"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"notes","title":"Notes","version":"0.1.0","kind":"scoped","symbol":"note","selectedSymbol":"note.fill","surface":"web","page":"ui/index.html"}`
	for name, body := range map[string]string{"plugin.json": manifest, "ui/index.html": "<p>Notes</p>"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		state := ctx.Value(turnKey{}).(*turnState)
		if strings.HasPrefix(state.work.Message.ID, "answer:") {
			return agentruntime.Result{Text: "Installed"}, nil
		}
		// A bot can't post its own install card from ask_user.
		if _, err := botCall(ctx, d, "ask_user", `{"question":"Install?","options":[{"id":"_install","label":"Yes"},{"id":"no","label":"No"}]}`); err == nil {
			t.Error("ask_user accepted the install answer")
		}
		_, err := botCall(ctx, d, "install_plugin", fmt.Sprintf(`{"path":%q}`, folder))
		return agentruntime.Result{Text: "NO_RESPONSE"}, err
	}}
	api, _ := startTest(t, f)
	botSend(t, api, "assistant", "plugin-turn", "Make a notes panel")
	run := awaitRun(t, api.Store, store.NewRun("plugin-turn", "assistant", "assistant", "message").ID, "completed")
	if run.Error != nil {
		t.Fatal(*run.Error)
	}
	messages, err := api.Store.Messages(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	card := messages[len(messages)-1]
	if len(card.Parts) != 1 || card.Parts[0].Kind != contract.PartKindChoice {
		t.Fatalf("no install card: %+v", card)
	}
	if list, err := api.plugins.List(t.Context()); err != nil || len(list.Installed) != 0 {
		t.Fatalf("installed before anyone answered: %+v %v", list, err)
	}
	answer := func() *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		api.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages/"+card.ID+"/answer", strings.NewReader(`{"optionID":"_install"}`)))
		return r
	}
	for range 2 {
		if r := answer(); r.Code != http.StatusOK {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	list, err := api.plugins.List(t.Context())
	if err != nil || len(list.Installed) != 1 {
		t.Fatalf("not installed: %+v %v", list, err)
	}
	plugin := list.Installed[0]
	if plugin.State != contract.PluginStateReady || !strings.HasPrefix(plugin.Source, "Made by ") || len(plugin.Chats) != 1 || plugin.Chats[0] != "assistant" {
		t.Fatalf("installed as %+v", plugin)
	}
	r := httptest.NewRecorder()
	api.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/plugins/notes/ui/index.html", nil))
	if r.Code != http.StatusOK || r.Body.String() != "<p>Notes</p>" {
		t.Fatal(r.Code, r.Body.String())
	}
}
