package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestClientHubRelaysToReportingDevice(t *testing.T) {
	var hub clientHub
	if _, err := hub.pick("phone", "home"); err == nil {
		t.Fatal("picked a device before any reported")
	}
	hub.report("phone", contract.ClientState{Platform: "ios", Timezone: "Australia/Sydney", Capabilities: []string{"home", "location"}})
	hub.report("mac", contract.ClientState{Platform: "macos", Timezone: "Asia/Manila", Capabilities: []string{"location"}})
	if _, err := hub.pick("phone", "home"); err == nil {
		t.Fatal("picked a device that is not listening")
	}
	phone, _ := hub.attach("phone")
	mac, _ := hub.attach("mac")
	// The sender wins when it allows the connection; otherwise another device that does.
	if id, _ := hub.pick("mac", "location"); id != "mac" {
		t.Fatalf("location went to %q, want the sending mac", id)
	}
	hub.report("mac", contract.ClientState{Platform: "macos", Timezone: "Asia/Manila", Capabilities: []string{}})
	if id, err := hub.pick("mac", "location"); err == nil {
		t.Fatalf("location fell back to %q instead of the sending mac", id)
	}
	if id, _ := hub.pick("", "location"); id != "phone" {
		t.Fatalf("routine location went to %q, want the phone", id)
	}
	if id, _ := hub.pick("mac", "home"); id != "phone" {
		t.Fatalf("home went to %q, want the phone", id)
	}
	if _, err := hub.pick("mac", "alarms"); err == nil || !strings.Contains(err.Error(), "Settings > Connections") {
		t.Fatalf("alarms error = %v", err)
	}

	done := make(chan contract.ClientResult)
	go func() {
		result, err := hub.call(t.Context(), "phone", contract.ClientRequest{ID: "r1", Capability: "home", Input: `{"action":"list"}`})
		if err != nil {
			t.Error(err)
		}
		done <- result
	}()
	request := <-phone
	if request.ID != "r1" || request.Capability != "home" {
		t.Fatalf("request = %+v", request)
	}
	if hub.answer("mac", "r1", contract.ClientResult{Output: ptr(`{"forged":true}`)}) {
		t.Fatal("accepted an answer from another device")
	}
	if !hub.answer("phone", "r1", contract.ClientResult{Output: ptr(`{"homes":[]}`)}) {
		t.Fatal("rejected the device's own answer")
	}
	if result := <-done; value(result.Output) != `{"homes":[]}` {
		t.Fatalf("result = %+v", result)
	}
	if hub.answer("phone", "r1", contract.ClientResult{}) {
		t.Fatal("accepted an answer after the call finished")
	}
	select {
	case extra := <-mac:
		t.Fatalf("mac received %+v", extra)
	default:
	}
}

func TestClientConnectionsRequireConsentAndPresence(t *testing.T) {
	var hub clientHub
	state := contract.ClientState{Platform: "macos", Timezone: "Asia/Manila", Capabilities: []string{}}
	hub.report("mac", state)
	stream, _ := hub.attach("mac")
	if len(hub.allowed()) != 0 {
		t.Fatal("connecting a client granted device access")
	}
	if _, err := hub.pick("mac", "location"); err == nil {
		t.Fatal("picked a device without location consent")
	}

	state.Capabilities = []string{"location"}
	hub.report("mac", state)
	if id, err := hub.pick("mac", "location"); err != nil || id != "mac" {
		t.Fatalf("allowed device = %q, %v", id, err)
	}
	hub.detach("mac", stream)
	if len(hub.online()) != 0 {
		t.Fatal("disconnected client is still online")
	}
	if _, err := hub.pick("mac", "location"); err == nil {
		t.Fatal("picked a disconnected device")
	}
	if _, err := hub.call(t.Context(), "mac", contract.ClientRequest{ID: "offline", Capability: "location", Input: `{}`}); err == nil {
		t.Fatal("relayed a request to a disconnected device")
	}

	stream, _ = hub.attach("mac")
	defer hub.detach("mac", stream)
	if id, err := hub.pick("mac", "location"); err != nil || id != "mac" {
		t.Fatalf("reconnected device = %q, %v", id, err)
	}
	state.Capabilities = []string{}
	hub.report("mac", state)
	if len(hub.allowed()) != 0 {
		t.Fatal("revoked connection is still offered to agents")
	}
	if _, err := hub.pick("mac", "location"); err == nil {
		t.Fatal("picked a device after consent was revoked")
	}
}

func TestClientContextNamesDeviceAndLocalTime(t *testing.T) {
	s := &Server{}
	s.clients.report("phone", contract.ClientState{Platform: "ios", Timezone: "Australia/Sydney", Capabilities: []string{"location"}})
	text := "hello"
	m := s.clientContext(t.Context(), store.Work{Trigger: contract.RunTriggerMessage, DeviceID: "phone"}, contract.Message{Parts: []contract.Part{{Kind: contract.PartKindText, Text: &text}}})
	first := value(m.Parts[0].Text)
	for _, want := range []string{"The user sent this from", "(iOS)", "Australia/Sydney", "location"} {
		if !strings.Contains(first, want) {
			t.Fatalf("context %q lacks %q", first, want)
		}
	}
	if s.userZone(store.Work{Trigger: contract.RunTriggerRoutine}) != "Australia/Sydney" {
		t.Fatal("routine turns should fall back to the last device's zone")
	}
}

func ptr(s string) *string { return &s }

// The app decodes capabilities as a list, so an empty report must not come back as null.
func TestReportClientReturnsEmptyList(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.reportClient(w, httptest.NewRequest("PUT", "/client", strings.NewReader(`{"platform":"macos","timezone":"Asia/Manila","capabilities":[]}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"capabilities":[]`) {
		t.Fatalf("report = %d %s", w.Code, w.Body)
	}
}

// A network drop can leave the stream looking open, so a silent device must still read as offline.
func TestClientPresenceNeedsCheckIns(t *testing.T) {
	var hub clientHub
	hub.report("phone", contract.ClientState{Platform: "ios", Timezone: "Asia/Manila", Capabilities: []string{"home"}})
	hub.attach("phone")
	if got := hub.online(); len(got) != 1 {
		t.Fatalf("online = %v, want the phone", got)
	}
	hub.devices["phone"].seen = time.Now().Add(-time.Minute)
	if got := hub.online(); len(got) != 0 {
		t.Fatalf("online = %v after missed check-ins", got)
	}
	if _, err := hub.pick("phone", "home"); err == nil {
		t.Fatal("picked a device that stopped checking in")
	}
}
