package vault

import (
	"errors"
	"strings"
	"testing"
)

func TestBitwardenJSON(t *testing.T) {
	data := []byte(`{
		"encrypted": false,
		"folders": [{"id":"f1","name":"Work"}],
		"items": [
			{"type":1,"name":"Login","folderId":"f1","notes":"note","login":{"username":"user","password":"pass","totp":"JBSWY3DPEHPK3PXP","uris":[{"uri":"https://example.com"},{"uri":"https://second.example"}]},"fields":[{"name":"Department","value":"Engineering"},{"name":"Recovery codes","value":"one\ntwo"}]},
			{"type":3,"name":"Card","card":{"cardholderName":"Holder","brand":"Visa","number":"4111111111111111","expMonth":"12","expYear":"2030","code":"123"}},
			{"type":2,"name":"Note","notes":"secret note"},
			{"type":4,"name":"Identity","identity":{"firstName":"Ada","lastName":"Lovelace"}}
		]
	}`)
	items, err := ParseImport(data, nil)
	if err != nil || len(items) != 4 {
		t.Fatal("unexpected Bitwarden import", len(items), err)
	}
	byTitle := map[string]bool{}
	for _, item := range items {
		byTitle[item.Title] = true
		switch item.Title {
		case "Login":
			if item.Kind != "login" || item.Fields["username"] != "user" || item.Fields["password"] != "pass" ||
				item.Fields["totp"] != "JBSWY3DPEHPK3PXP" || len(item.Codes) != 2 ||
				!strings.Contains(item.Fields["notes"], "Folder: Work") ||
				!strings.Contains(item.Fields["notes"], "Department: Engineering") ||
				!strings.Contains(item.Fields["notes"], "URL: https://second.example") {
				t.Fatalf("login = %+v", item)
			}
		case "Card":
			if item.Kind != "card" || item.Fields["holder"] != "Holder" || item.Fields["number"] != "4111111111111111" ||
				item.Fields["expiry"] != "12/2030" || item.Fields["cvc"] != "123" {
				t.Fatalf("card = %+v", item)
			}
		case "Identity":
			if item.Kind != "note" || !strings.Contains(item.Fields["notes"], "firstName: Ada") {
				t.Fatalf("identity = %+v", item)
			}
		}
	}
	if !byTitle["Note"] {
		t.Fatal("missing secure note")
	}
	if _, err := ParseImport([]byte(`{"encrypted":true,"items":[{"type":1,"name":"x"}]}`), nil); !errors.Is(err, ErrVendorEncrypted) {
		t.Fatal("encrypted Bitwarden JSON was accepted:", err)
	}
	if _, err := ParseImport([]byte(`{"encrypted":true,"data":"encrypted-payload"}`), nil); !errors.Is(err, ErrVendorEncrypted) {
		t.Fatal("encrypted Bitwarden payload was accepted:", err)
	}
	if _, err := ParseImport([]byte(`{"something":"unknown"}`), nil); err == nil {
		t.Fatal("unknown JSON object was accepted")
	}
}

func TestVendorErrorsDoNotEchoValues(t *testing.T) {
	data := []byte(`{"encrypted":true,"items":[],"password":"do-not-echo-this"}`)
	_, err := ParseImport(data, nil)
	if err == nil || strings.Contains(err.Error(), "do-not-echo-this") {
		t.Fatal("vendor error exposed a value")
	}
}
