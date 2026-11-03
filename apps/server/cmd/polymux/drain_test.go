package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDrainSafety(t *testing.T) {
	for _, test := range []struct {
		name, instance string
		active         int
	}{
		{name: "process changed", instance: "replacement"},
		{name: "work remains", instance: "original", active: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var cancelled atomic.Bool
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input map[string]string
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					return
				}
				value := maintenance{State: "draining", Instance: "original"}
				switch input["action"] {
				case "seal":
					value.State, value.Instance, value.Active = "ready", test.instance, test.active
				case "cancel":
					cancelled.Store(true)
					value.State = "running"
				}
				if err := json.NewEncoder(w).Encode(value); err != nil {
					t.Error(err)
				}
			}))
			defer api.Close()
			token := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(token, []byte("test-token"), 0600); err != nil {
				t.Fatal(err)
			}
			err := drain([]string{"--url", api.URL, "--token-file", token, "--id", "test-update-identity"})
			if err == nil || !cancelled.Load() {
				t.Fatalf("unsafe seal accepted or admission not resumed: %v", err)
			}
		})
	}
}

func TestDrainVerification(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"state":"running","buildSHA":"candidate"}`))
	}))
	defer api.Close()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	err := drain([]string{"--url", api.URL, "--token-file", token, "--status", "--build", "candidate", "--ready"})
	if err == nil || !strings.Contains(err.Error(), "not sealed") {
		t.Fatalf("unsealed candidate passed health: %v", err)
	}
}

func TestMaintenanceCall(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		valid      bool
	}{
		{name: "ready", body: `{"state":"ready","active":0,"blocked":0,"buildSHA":"candidate"}`, status: 200, valid: true},
		{name: "old server", status: 404},
		{name: "unauthorized", status: 401},
		{name: "another updater", status: 409},
		{name: "missing state", body: `{}`, status: 200},
		{name: "malformed", body: `broken`, status: 200},
		{name: "redirect", status: 302},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/maintenance" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("maintenance request lost its path, method, or credential")
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["id"] != "test-update-identity" || body["action"] != "seal" {
					t.Error("maintenance request lost its identity or action")
				}
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer api.Close()
			value, err := maintenanceCall(t.Context(), api.URL, "test-token", "test-update-identity", "seal")
			if (err == nil) != test.valid {
				t.Fatalf("response %+v, error %v", value, err)
			}
		})
	}
}
