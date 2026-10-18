package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
)

func TestNetworkDescribesServer(t *testing.T) {
	_, endpoint := startTest(t, &testFactory{})
	response, err := http.Get(endpoint.URL + "/network")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	var value struct {
		Server struct {
			Name, Build, Platform, Role string
		}
		Devices []any
	}
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	if value.Server.Name == "" || value.Server.Role != "server" || value.Server.Platform == "" {
		t.Fatalf("server %+v", value.Server)
	}
	if value.Devices == nil {
		t.Fatal("devices must be an array")
	}
}

func TestNetworkInvite(t *testing.T) {
	api, endpoint := startTest(t, &testFactory{})
	if err := os.MkdirAll(filepath.Join(api.config.DataDir, "devices"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(api.config.DataDir, "devices", "local-token"), []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(endpoint.URL + "/network/invite")
	if err != nil {
		t.Fatal(err)
	}
	var invite struct{ URL, Token, Ticket string }
	if err := json.NewDecoder(response.Body).Decode(&invite); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if invite.URL == "" || invite.Token != "token" || invite.Ticket != "" {
		t.Fatalf("invite %+v", invite)
	}
	api.config.Ticket = func(time.Duration) string { return "endpointfixture" }
	response, err = http.Get(endpoint.URL + "/network/invite")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(&invite); err != nil || invite.Ticket != "endpointfixture" {
		t.Fatalf("invite omitted the tunnel ticket: %+v %v", invite, err)
	}
}

func TestNetworkDevices(t *testing.T) {
	dir := t.TempDir()
	pairs, err := device.Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer pairs.Close()
	local, err := pairs.Local(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	broker := device.New(pairs)
	phone, err := broker.Issue(t.Context(), "Polymux app")
	if err != nil {
		t.Fatal(err)
	}
	s := settingsAPI(t)
	s.config.Devices = broker
	api := httptest.NewServer(s.Handler())
	defer api.Close()
	call := func(token, method, path, body string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, api.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := api.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Body.Close() })
		return response
	}
	if response := call(phone, "PATCH", "/network/devices/self", `{"name":"Phone"}`); response.StatusCode != 204 {
		t.Fatalf("rename status %d", response.StatusCode)
	}
	var network struct {
		Current string
		Devices []struct{ ID, Name string }
	}
	if err := json.NewDecoder(call(phone, "GET", "/network", "").Body).Decode(&network); err != nil {
		t.Fatal(err)
	}
	var phoneID string
	for _, d := range network.Devices {
		if d.Name == "Phone" {
			phoneID = d.ID
		}
	}
	if phoneID == "" || network.Current != phoneID || len(network.Devices) != 2 {
		t.Fatalf("network %+v", network)
	}
	if response := call(phone, "DELETE", "/network/devices/"+phoneID, ""); response.StatusCode != 409 {
		t.Fatalf("self removal status %d", response.StatusCode)
	}
	if response := call(local, "DELETE", "/network/devices/"+phoneID, ""); response.StatusCode != 204 {
		t.Fatalf("remove status %d", response.StatusCode)
	}
	if response := call(phone, "GET", "/network", ""); response.StatusCode != 401 {
		t.Fatalf("removed device status %d", response.StatusCode)
	}
}
