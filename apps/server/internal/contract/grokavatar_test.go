package contract

import "testing"

func TestGrokBotAvatarDesign(t *testing.T) {
	design := GrokBotAvatarDesign("blob", "violet")
	if design == nil || design.Shape != "galet" || design.Color != "violet" {
		t.Fatalf("echo mapping %+v", design)
	}
	design = GrokBotAvatarDesign("wedge", "-")
	if design == nil || design.Shape != "triangle" || design.Color != "encre" {
		t.Fatalf("empty color fallback %+v", design)
	}
	if GrokBotAvatarDesign("", "") != nil {
		t.Fatal("expected nil for empty profile")
	}
}

func TestGrokBotAvatarFromDefinitionJSON(t *testing.T) {
	raw := []byte(`{"prompt":"hi","avatarShape":"hex","avatarColor":"orange"}`)
	design := GrokBotAvatarFromDefinition(raw)
	if design == nil || design.Shape != "hexagone" || design.Color != "orange" {
		t.Fatalf("definition parse %+v", design)
	}
}
