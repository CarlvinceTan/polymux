package store

import (
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestRevokePush(t *testing.T) {
	s := runStore(t)
	ctx := t.Context()
	for _, device := range []struct{ id, pairing string }{{"installation-revoked", "pair-a"}, {"installation-kept-1", "pair-b"}} {
		if err := s.RegisterPush(ctx, contract.PushDevice{ID: device.id, Token: strings.Repeat("ab", 32), Environment: "sandbox", Server: "http://server.test", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		if err := s.BindPush(ctx, device.id, device.pairing); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RevokePush(ctx, "pair-a"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM push_devices WHERE enabled=1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var enabled []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		enabled = append(enabled, id)
	}
	if len(enabled) != 1 || enabled[0] != "installation-kept-1" {
		t.Fatalf("enabled after revocation: %v", enabled)
	}
}
