package store

import (
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestRemapProviders(t *testing.T) {
	s := runStore(t)
	agents := []contract.Agent{
		{ID: "moved", Name: "Moved", Provider: "two", Fallback: &contract.AgentFallback{Mode: "custom", Models: []contract.FallbackModel{{Provider: "two", Model: "m"}, {Provider: "keep", Model: "n"}}}},
		{ID: "stays", Name: "Stays", Provider: "keep"},
	}
	if err := s.Register(t.Context(), agents); err != nil {
		t.Fatal(err)
	}
	if err := s.RemapProviders(t.Context(), map[string]string{"two": "one"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RemapProviders(t.Context(), map[string]string{"two": "one"}); err != nil {
		t.Fatal("repeating a remap should change nothing")
	}
	got, err := s.Agents(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		switch a.ID {
		case "moved":
			if a.Provider != "one" || a.Fallback == nil || a.Fallback.Models[0].Provider != "one" || a.Fallback.Models[1].Provider != "keep" {
				t.Fatalf("the bot and its fallbacks should follow the merged provider: %+v", a)
			}
		case "stays":
			if a.Provider != "keep" {
				t.Fatalf("an unrelated bot changed: %+v", a)
			}
		}
	}
}
