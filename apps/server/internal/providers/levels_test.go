package providers

import (
	"slices"
	"testing"

	"github.com/MichaelKinsy/PiG/ai"
)

func TestWithLevels(t *testing.T) {
	all, _ := Builtin("deepseek").GetModels()
	want := []string{}
	for _, level := range ai.GetSupportedThinkingLevels(all[0]) {
		want = append(want, string(level))
	}
	stored := []Provider{
		{Type: "deepseek", Models: []Model{{ID: all[0].ID}, {ID: "unlisted"}}},
		{Type: "custom", Models: []Model{{ID: "local"}}},
	}
	got := WithLevels(stored)
	if !slices.Equal(got[0].Models[0].Reasoning, want) {
		t.Fatalf("levels %v, want %v", got[0].Models[0].Reasoning, want)
	}
	if got[0].Models[1].Reasoning != nil {
		t.Fatalf("unknown models got levels: %+v", got)
	}
	if !slices.Equal(got[1].Models[0].Reasoning, LocalLevels) {
		t.Fatalf("local levels %v, want %v", got[1].Models[0].Reasoning, LocalLevels)
	}
	deeper := WithLevels([]Provider{{Type: "custom", Models: []Model{{ID: "local", Reasoning: []string{"low", "medium", "xhigh"}}}}})
	if !slices.Equal(deeper[0].Models[0].Reasoning, []string{"low", "medium", "xhigh"}) {
		t.Fatalf("declared levels replaced: %v", deeper[0].Models[0].Reasoning)
	}
	if stored[0].Models[0].Reasoning != nil {
		t.Fatal("stored settings changed")
	}
}
