package providers

import (
	"slices"
	"testing"
)

func TestMigrateMergesAccounts(t *testing.T) {
	t.Parallel()
	model := Model{ID: "shared", Tags: []string{"text"}, Enabled: true}
	list := []Provider{
		{ID: "one", Type: "deepseek", Name: "Work", Enabled: true, Order: 0, Models: []Model{model}},
		{ID: "mine", Type: "custom", Name: "Mine", Enabled: true, Order: 1},
		{ID: "two", Type: "deepseek", Name: "Personal", Enabled: false, Order: 2, Models: []Model{model, {ID: "extra", Enabled: true}}},
		{ID: "also", Type: "custom", Name: "Also", Enabled: true, Order: 3},
	}
	out, remap := Migrate(list)
	if len(out) != 3 || out[0].ID != "one" || out[1].ID != "mine" || out[2].ID != "also" {
		t.Fatalf("expected one DeepSeek provider and both custom ones: %+v", out)
	}
	if remap["two"] != "one" || len(remap) != 1 {
		t.Fatalf("retired record not mapped to the provider that took it: %v", remap)
	}
	merged := out[0]
	if merged.Name != Builtin("deepseek").Name || !merged.Enabled || len(merged.Models) != 2 {
		t.Fatalf("merged provider: %+v", merged)
	}
	if got := merged.Connections; len(got) != 2 || got[0].ID != "one" || got[0].Name != "Work" || got[1].ID != "two" || got[1].Name != "Personal" {
		t.Fatalf("each record should become a connection that keeps its ID: %+v", got)
	}
	if merged.Connections[0].Enabled != true || merged.Connections[1].Enabled {
		t.Fatalf("a record that was off must stay off as a connection: %+v", merged.Connections)
	}
	if got := out[1].Connections; len(got) != 1 || got[0].ID != "mine" || got[0].Name != "" {
		t.Fatalf("a custom provider keeps one connection under its own ID: %+v", got)
	}
	for i, p := range out {
		if p.Order != i {
			t.Fatal("providers should be numbered in order")
		}
	}
	again, remapped := Migrate(out)
	if len(remapped) != 0 || !slices.EqualFunc(again, out, func(a, b Provider) bool { return a.ID == b.ID && len(a.Connections) == len(b.Connections) }) {
		t.Fatal("migrating twice changed something")
	}
}

func TestActiveAndKeyID(t *testing.T) {
	t.Parallel()
	p := Provider{ID: "p", Enabled: true, Connections: []Connection{{ID: "p"}, {ID: "p~2", Enabled: true}}}
	if got := Active(p); len(got) != 1 || got[0].ID != "p~2" || KeyID(p) != "p~2" || !Usable(p) {
		t.Fatalf("only an enabled connection is used: %+v", got)
	}
	p.Enabled = false
	if Active(p) != nil || Usable(p) {
		t.Fatal("a disabled provider has no active connections")
	}
	if KeyID(Provider{ID: "bare"}) != "bare" {
		t.Fatal("a provider without connections falls back to its own ID")
	}
}

func TestMigrateKeepsDisabledProviderConnectionsOn(t *testing.T) {
	t.Parallel()
	out, _ := Migrate([]Provider{{ID: "a", Type: "deepseek", Name: "A", Order: 0}, {ID: "b", Type: "deepseek", Name: "B", Order: 1}})
	if len(out) != 1 || out[0].Enabled || !out[0].Connections[0].Enabled || !out[0].Connections[1].Enabled {
		t.Fatalf("a provider that was off should keep its connections ready: %+v", out)
	}
}
