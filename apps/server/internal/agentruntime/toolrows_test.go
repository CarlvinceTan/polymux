package agentruntime

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MichaelKinsy/PiG/ai"
)

// The chat row data the tool events carry: a bounded, redacted argument summary and a
// result excerpt capped at 2 KB and valid UTF-8.
func TestToolEventCarriesArgsSummary(t *testing.T) {
	if got := toolSummary("bash", json.RawMessage(`{"command":"API_KEY=sk-1 go test ./..."}`)); got != "[redacted] go test ./..." {
		t.Fatalf("summary %q", got)
	}
	if got := toolSummary("read", json.RawMessage(`{"path":"a/`+strings.Repeat("b", 400)+`.swift"}`)); len(got) > 200 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long summary %q", got)
	}
	if got := toolSummary("bash", nil); got != "" {
		t.Fatalf("empty summary %q", got)
	}
}

func TestToolOutputExcerptCapped(t *testing.T) {
	if got := toolExcerpt([]ai.ToolResultMessageContent{ai.TextContent{Text: strings.Repeat("line\n", 2000)}}); len(got) > 2048 || !strings.HasPrefix(got, "line\n") {
		t.Fatalf("excerpt %d bytes", len(got))
	}
	if got := toolExcerpt([]ai.ToolResultMessageContent{ai.TextContent{Text: strings.Repeat("é", 3000)}}); len(got) > 2048 || !utf8.ValidString(got) {
		t.Fatalf("multibyte excerpt %d bytes", len(got))
	}
}

func TestToolEventCarriesArgsSummaryRedTwin(t *testing.T) {
	if got := toolSummary("bash", json.RawMessage(`{"command":"echo sk-abcdefghijklmnop"}`)); got != "echo [redacted]" {
		t.Fatalf("credential escaped summary redaction")
	}
	if got := toolSummary("read", json.RawMessage(`{"path":42}`)); got != "" {
		t.Fatalf("non-string path yielded a summary")
	}
}

func TestToolOutputExcerptCappedRedTwin(t *testing.T) {
	if got := toolExcerpt([]ai.ToolResultMessageContent{ai.TextContent{Text: `password: test-only-value`}}); got != "[redacted]" {
		t.Fatal("credential escaped excerpt redaction")
	}
	for _, input := range []string{`Authorization: Bearer test-only-token`, `PASSWORD='test only value'`, `"api_key": "test only value"`} {
		if got := toolExcerpt([]ai.ToolResultMessageContent{ai.TextContent{Text: input}}); got != "[redacted]" {
			t.Fatal("quoted credential or authentication header escaped redaction")
		}
	}
	if got := toolExcerpt(nil); got != "" {
		t.Fatal("absent output yielded an excerpt")
	}
}
