package runtime

import (
	"slices"
	"testing"

	"github.com/MichaelKinsy/PiG/ai"
)

func TestMaxThinking(t *testing.T) {
	for _, test := range []struct {
		levels []string
		want   ai.ModelThinkingLevel
	}{
		{nil, ai.ThinkingHigh},
		{[]string{"off", "low", "medium", "high"}, ai.ThinkingHigh},
		{[]string{"low", "medium", "xhigh"}, ai.ThinkingXHigh},
		{[]string{"off"}, ai.ThinkingOff},
	} {
		if got := maxThinking(test.levels); got != test.want {
			t.Fatalf("maxThinking(%v) = %q, want %q", test.levels, got, test.want)
		}
	}
}

func TestSpecModelLevelsSetMaxThinking(t *testing.T) {
	registry := newSpecRegistry(ProviderSpec{ID: "local", BaseURL: "http://127.0.0.1:1/v1", Models: []string{"qwen", "plain"},
		Levels: map[string][]string{"qwen": {"off", "low", "medium", "high", "xhigh"}}})
	qwen := registry.list()
	var max ai.ThinkingLevel
	for _, model := range qwen {
		if model.ID == "qwen" {
			max = model.Capabilities.MaxThinking
			if !model.ProviderMeta.Reasoning {
				t.Fatal("qwen does not reason")
			}
		}
		if model.ID == "plain" && (model.Capabilities.MaxThinking != ai.ThinkingLevel(ai.ThinkingHigh) || !model.ProviderMeta.Reasoning) {
			t.Fatalf("plain max = %q", model.Capabilities.MaxThinking)
		}
	}
	if max != ai.ThinkingLevel(ai.ThinkingXHigh) {
		t.Fatalf("qwen max = %q", max)
	}
}

// A model tagged as seeing pictures gets them; any other gets a placeholder from PiG.
func TestSpecVisionTakesImages(t *testing.T) {
	registry := newSpecRegistry(ProviderSpec{ID: "custom", BaseURL: "http://127.0.0.1:1/v1", Models: []string{"eyes", "blind"},
		Vision: map[string]bool{"eyes": true}})
	for _, model := range registry.list() {
		if sees := slices.Contains(model.Input, "image"); sees != (model.ID == "eyes") {
			t.Fatalf("%s input %v", model.ID, model.Input)
		}
	}
}
