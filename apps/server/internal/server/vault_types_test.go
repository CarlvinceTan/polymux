package server

import (
	"encoding/json"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"strings"
	"testing"
)

func TestVaultWire(t *testing.T) {
	input := contract.VaultItem{ID: "entry", Kind: "login", Title: "Example", Fields: map[string]string{"password": "synthetic-secret"}, Codes: []contract.VaultCode{{ID: "recovery", Value: "synthetic-code", Used: true}}, Revision: 7}
	output := wireItem(vaultItem(input))
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "synthetic-") {
		t.Fatal("wire adapter exposed values")
	}
	if output.Fields["password"] != "" || len(output.Fields) != 1 || output.Codes[0].Value != "" || !output.Codes[0].Used || output.Revision != 7 {
		t.Fatal("editor metadata lost")
	}
}
