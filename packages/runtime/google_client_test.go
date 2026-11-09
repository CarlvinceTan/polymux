package runtime

import (
	"testing"

	"github.com/MichaelKinsy/PiG/ai"
)

func TestGoogleFailureStatus(t *testing.T) {
	for reason, want := range map[string]int{
		`429 [{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}]`: 429,
		`400 [{"error":{"code":400}}]`:                               400,
		` 503 unavailable`:                                           503,
		`4000 tokens is too many`:                                    0,
		`200 ok`:                                                     0,
		`connection reset`:                                           0,
		``:                                                           0,
	} {
		if got := leadingStatus(reason); got != want {
			t.Errorf("leadingStatus(%q) = %d, want %d", reason, got, want)
		}
	}
	google := &ai.Model{ProviderMeta: ai.ProviderMetadata{API: ai.APIGoogleGenerativeAI}}
	vertex := &ai.Model{ProviderMeta: ai.ProviderMetadata{API: ai.APIGoogleVertex}}
	openai := &ai.Model{ProviderMeta: ai.ProviderMetadata{API: "openai-completions"}}
	if !ownsClient(google) || !ownsClient(vertex) || ownsClient(openai) {
		t.Fatal("only Google's adapters make their own requests")
	}
}

func TestGoogleEndpointUsesTheNativeAdapter(t *testing.T) {
	for url, want := range map[string]ai.API{
		"https://generativelanguage.googleapis.com/v1beta/openai": ai.APIGoogleGenerativeAI,
		"https://api.deepseek.com/v1":                             ai.APIOpenAICompletions,
	} {
		r := newModelRegistry(url, "key", Client{})
		got := r.api
		if r.builtin != nil {
			got = ai.APIGoogleGenerativeAI
		}
		if got != want {
			t.Errorf("%s served by %s, want %s", url, got, want)
		}
	}
}
