package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/plugins"
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

func TestPickInvite(t *testing.T) {
	ready := func(name, url string) plugins.Transport {
		return plugins.Transport{Plugin: name, State: contract.PluginStateReady, API: url}
	}
	transports := []plugins.Transport{
		{Plugin: "broken", State: "error", API: "https://broken.example"},
		ready("opentunnel", "https://opentunnel.example"),
		ready("tailscale", "https://tailscale.example"),
	}
	for name, test := range map[string]struct {
		preference, public string
		url                string
		found              bool
	}{
		"automatic prefers public":            {public: "https://public.example", url: "https://public.example", found: true},
		"automatic skips an unready plugin":   {url: "https://opentunnel.example", found: true},
		"iroh ignores public and plugins":     {preference: "iroh", public: "https://public.example"},
		"preferred plugin outranks the first": {preference: "tailscale", public: "https://public.example", url: "https://tailscale.example", found: true},
		"preferred public":                    {preference: "public", public: "https://public.example", url: "https://public.example", found: true},
		"preferred public without a URL":      {preference: "public", url: "https://opentunnel.example", found: true},
		"missing plugin falls back to public": {preference: "missing", public: "https://public.example", url: "https://public.example", found: true},
		"unready plugin falls back":           {preference: "broken", url: "https://opentunnel.example", found: true},
	} {
		t.Run(name, func(t *testing.T) {
			url, found := pickInvite(test.preference, test.public, transports)
			if url != test.url || found != test.found {
				t.Fatalf("pickInvite = %q, %v; want %q, %v", url, found, test.url, test.found)
			}
		})
	}
}

func TestIrohPreferredInvite(t *testing.T) {
	api := settingsAPI(t)
	api.config.PublicURL = "https://server.connect.example.com"
	transport := "iroh"
	if _, err := api.settings.Update(contract.SettingsPatch{Transport: &transport}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://server.local/network/invite", nil)
	value := api.invite(request, "one-time-code")
	if value["url"] != "http://server.local" || value["token"] != "one-time-code" || value["ticket"] != "" {
		t.Fatalf("iroh invitation = %v", value)
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
