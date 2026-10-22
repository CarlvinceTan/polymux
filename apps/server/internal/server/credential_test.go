package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/secrets"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
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
			http.Error(w, "missing credential", 401)
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, 302)
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
