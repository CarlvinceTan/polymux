package agentruntime

import (
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/skills"
)

func TestWorkspaceWithoutCheckout(t *testing.T) {
	prompt, err := buildPrompt(Definition{Workspace: t.TempDir(), Ephemeral: true}, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range []string{"detached conversation checkout", "This conversation's checkout"} {
		if strings.Contains(prompt, claim) {
			t.Errorf("prompt claims an unprepared workspace: %q", claim)
		}
	}
}

func TestPromptLayers(t *testing.T) {
	for _, test := range []struct {
		name, instructions, last string
	}{{"default", "", DefaultPrompt}, {"custom", "Reply only in French.", "Reply only in French."}} {
		t.Run(test.name, func(t *testing.T) {
			prompt, err := buildPrompt(Definition{Agent: contract.Agent{ID: "a", Name: "A"}, Prompt: test.instructions}, nil, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			library, system := strings.Index(prompt, "Agent policy"), strings.Index(prompt, "Polymux policy")
			if library != 0 || system < library || !strings.HasSuffix(prompt, test.last) {
				t.Fatalf("layers out of order: library %d, system %d", library, system)
			}
			if test.instructions != "" && strings.Contains(prompt, "How you talk") {
				t.Fatal("custom instructions should replace the default ones")
			}
			if !strings.Contains(prompt, "Keep your plumbing out") {
				t.Fatal("custom instructions must not remove the system layer")
			}
		})
	}
}

func TestStoredInstructions(t *testing.T) {
	if got := CustomPrompt("\n" + DefaultPrompt + " "); got != "" {
		t.Fatalf("the unedited default was stored: %q", got)
	}
	if got := CustomPrompt(DefaultPrompt + " Be blunt."); got == "" {
		t.Fatal("an edited default was dropped")
	}
	if got := StartPrompt("Run releases."); !strings.HasPrefix(got, DefaultPrompt) || !strings.HasSuffix(got, "Run releases.") {
		t.Fatalf("new bot instructions lost a part: %q", got)
	}
	if got := StartPrompt(" "); got != "" {
		t.Fatalf("a blank start was stored: %q", got)
	}
}

func TestCardsComeWithTheTool(t *testing.T) {
	installed := []skills.Skill{{Name: "polymux", Description: "About Polymux", Path: "/s/polymux/SKILL.md"}, {Name: skills.CardsName, Path: "/s/html-cards/REFERENCE.md"}}
	for _, test := range []struct {
		name          string
		tools         []string
		path, inlined bool
	}{{"read", []string{"show_html", "read"}, true, false}, {"no read", []string{"show_html"}, false, true}, {"no tool", []string{"read"}, false, false}} {
		t.Run(test.name, func(t *testing.T) {
			prompt, err := buildPrompt(Definition{Agent: contract.Agent{ID: "a"}}, test.tools, false, installed)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(prompt, `<skill name="html-cards"`) {
				t.Fatal("the card reference was listed as a skill")
			}
			// With read the reference loads by path; without it, it's inline, since cards are too frequent for a load each.
			if strings.Contains(prompt, "/s/html-cards/REFERENCE.md") != test.path || strings.Contains(prompt, "polymuxSend") != test.inlined {
				t.Fatalf("card reference delivered wrongly: %q", prompt[:200])
			}
		})
	}
}

func TestVoiceOnlyInDirectChat(t *testing.T) {
	ack := "your only voice"
	tools := []string{"send_to_user"}
	for _, test := range []struct {
		name  string
		scope string
		want  bool
	}{{"direct", "", true}, {"group", "group-1", false}, {"peer", "peer-1", false}, {"child", "child-1", false}, {"voice", "voice-1", false}} {
		t.Run(test.name, func(t *testing.T) {
			prompt, err := buildPrompt(Definition{Agent: contract.Agent{ID: "a"}, Scope: test.scope}, tools, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(prompt, ack) != test.want {
				t.Fatalf("voice guidance present = %v, want %v", !test.want, test.want)
			}
		})
	}
}
