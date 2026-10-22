package server

import (
	"slices"
	"strings"
	"testing"
)

func TestPeerSchema(t *testing.T) {
	for _, test := range []struct {
		name    string
		tool    peerTool
		intents []string
		want    []string
		omit    []string
	}{
		{
			name:    "direct",
			intents: []string{"request", "question", "result", "status", "fyi"},
			want:    []string{"Messages appear in both chats", "fyi only records a note"},
		},
		{
			name:    "private direct",
			tool:    peerTool{private: true},
			intents: []string{"request", "question", "result", "status"},
			want: []string{
				"result/status add private notes", "return to the spokesperson",
				"do not appear in typed chats", "cannot recursively request",
			},
			omit: []string{"fyi", "Messages appear in both chats", "wake it in its own chat"},
		},
		{
			name: "group",
			tool: peerTool{handoff: true},
			want: []string{"Members can speak again later", "six hops"},
		},
		{
			name: "private group",
			tool: peerTool{handoff: true, private: true},
			want: []string{
				"another current group member", "end your stage", "at most once",
				"return to the spokesperson", "do not appear in the typed group chat",
			},
			omit: []string{"speak again later", "six hops"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := test.tool.Schema()
			properties := schema.Parameters["properties"].(map[string]any)
			intent, offered := properties["intent"]
			if offered == test.tool.handoff {
				t.Fatalf("intent offered=%v, handoff=%v", offered, test.tool.handoff)
			}
			if offered {
				intents := intent.(map[string]any)["enum"].([]string)
				if !slices.Equal(intents, test.intents) {
					t.Fatalf("offered intents=%v, want %v", intents, test.intents)
				}
			}
			for _, want := range test.want {
				if !strings.Contains(schema.Description, want) {
					t.Errorf("description omitted %q", want)
				}
			}
			for _, omit := range test.omit {
				if strings.Contains(schema.Description, omit) {
					t.Errorf("description advertised %q", omit)
				}
			}
		})
	}
}
