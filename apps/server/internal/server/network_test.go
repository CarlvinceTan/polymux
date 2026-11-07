package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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
	response, err := http.Get(endpoint.URL + "/network/invite")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatal("pairing without a broker exposed an owner credential")
	}
	pairs, err := device.Open(t.Context(), t.TempDir(), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer pairs.Close()
	api.config.Devices = device.New(pairs)
	response, err = http.Get(endpoint.URL + "/network/invite")
	if err != nil {
		t.Fatal(err)
	}
	var invite struct{ URL, Token, Ticket string }
	if err := json.NewDecoder(response.Body).Decode(&invite); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if invite.URL == "" || len(invite.Token) != 43 || invite.Ticket != "" {
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

func TestPublicInvite(t *testing.T) {
	api, endpoint := startTest(t, &testFactory{})
	api.config.PublicURL = "https://server.connect.example.com"
	api.config.Ticket = func(time.Duration) string {
		t.Fatal("HTTPS pairing must not wait for an Iroh ticket")
		return ""
	}
	request := httptest.NewRequest(http.MethodGet, endpoint.URL+"/network/invite", nil)
	request.Host = "attacker.example"
	request.Header.Set("X-Forwarded-Host", "attacker.example")
	value := api.invite(request, "one-time-code")
	if value["url"] != api.config.PublicURL || value["token"] != "one-time-code" || value["ticket"] != "" {
		t.Fatalf("public invitation = %v", value)
	}
}

func TestNetworkDevices(t *testing.T) {
	s := settingsAPI(t)
	dir := s.config.DataDir
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
		Server        struct{ Name string }
		Current, Host string
		Online        []string
		Devices       []struct{ ID, Name, Label string }
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
	owner, err := pairs.Authenticate(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	if network.Host != owner.ID {
		t.Fatalf("server credential is not the host: %+v", network)
	}
	if !slices.Contains(network.Online, owner.ID) || slices.Contains(network.Online, phoneID) {
		t.Fatalf("server presence must not depend on a local app: %+v", network)
	}
	for _, d := range network.Devices {
		if d.ID == owner.ID && d.Name != network.Server.Name {
			t.Fatalf("server device has a placeholder name: %+v", d)
		}
	}
	if response := call(
		phone,
		"DELETE",
		"/network/devices/"+owner.ID,
		"",
	); response.StatusCode != 409 {
		t.Fatalf("server removal status %d", response.StatusCode)
	}
	if response := call(
		local,
		"GET",
		"/network",
		"",
	); response.StatusCode != 200 {
		t.Fatalf("server credential was revoked: %d", response.StatusCode)
	}
	if response := call(
		phone,
		"PATCH",
		"/network/devices/"+owner.ID,
		`{"label":"Home server"}`,
	); response.StatusCode != 204 {
		t.Fatalf("server label status %d", response.StatusCode)
	}
	if err := json.NewDecoder(call(phone, "GET", "/network", "").Body).Decode(&network); err != nil {
		t.Fatal(err)
	}
	for _, d := range network.Devices {
		if d.ID != owner.ID {
			continue
		}
		if d.Name != network.Server.Name || d.Label != "Home server" {
			t.Fatalf("server label or hostname lost: %+v", d)
		}
	}
	label := func(body string) string {
		if response := call(local, "PATCH", "/network/devices/"+phoneID, body); response.StatusCode != 204 {
			t.Fatalf("label status %d", response.StatusCode)
		}
		call(phone, "PATCH", "/network/devices/self", `{"name":"Phone"}`)
		var listed struct {
			Devices []struct{ ID, Name, Label string }
		}
		if err := json.NewDecoder(call(phone, "GET", "/network", "").Body).Decode(&listed); err != nil {
			t.Fatal(err)
		}
		for _, d := range listed.Devices {
			if d.ID == phoneID {
				if d.Name != "Phone" {
					t.Fatalf("hostname lost: %+v", d)
				}
				return d.Label
			}
		}
		return "missing"
	}
	if got := label(`{"label":"Work phone"}`); got != "Work phone" {
		t.Fatalf("label %q", got)
	}
	if got := label(`{"label":""}`); got != "" {
		t.Fatalf("cleared label %q", got)
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
