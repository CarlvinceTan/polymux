package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestMCPLimit(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for index := 0; index < 65; index++ {
		name := fmt.Sprintf("connection-%d", index)
		raw, err := json.Marshal(map[string]string{"name": name, "command": "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		err = s.RegisterMCP(t.Context(), name, raw, "", nil, nil)
		if index == 64 {
			if !errors.Is(err, ErrMCPLimit) {
				t.Fatal("registry cap was not enforced", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	raw := json.RawMessage(`{"name":"connection-0","command":"updated"}`)
	if err := s.RegisterMCP(t.Context(), "connection-0", raw, "", nil, nil); err != nil {
		t.Fatal("existing setup could not update at cap", err)
	}
	records, err := s.MCPServers(t.Context())
	if err != nil || len(records) != 64 {
		t.Fatal("failed registration changed inventory", len(records), err)
	}
}
