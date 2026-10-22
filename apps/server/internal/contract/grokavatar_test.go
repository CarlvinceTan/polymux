package contract

import "testing"

func TestGrokBotAvatarDesign(t *testing.T) {
	design := GrokBotAvatarDesign("blob", "violet")
	if design == nil || design.Shape != "cercle" || design.Color != "violet" {
		t.Fatalf("echo mapping %+v", design)
	}
	design = GrokBotAvatarDesign("wedge", "-")
	if design == nil || design.Shape != "wedge" || design.Color != "encre" {
		t.Fatalf("empty color fallback %+v", design)
	}
	if GrokBotAvatarDesign("", "") != nil {
		t.Fatal("expected nil for empty profile")
	}
}

func TestGrokCreationAvatars(t *testing.T) {
	for source, shape := range map[string]string{
		"blob": "cercle", "cloud": "nuage", "square": "square", "sparkle": "sparkle",
		"clover": "clover", "heart": "heart", "flower": "flower", "teardrop": "goutte",
		"tablet": "tablet", "wedge": "wedge", "house": "house", "star": "star", "hex": "hexagone",
	} {
		design := GrokBotAvatarDesign(source, "yellow")
		if design == nil || design.Shape != shape || design.Color != "ambre" {
			t.Errorf("%s mapped to %+v", source, design)
		}
	}
	for source, color := range map[string]string{
		"black": "encre", "brown": "brun", "red": "rouge", "orange": "orange",
		"yellow": "ambre", "green": "vert", "cyan": "turquoise", "blue": "bleu",
		"violet": "violet", "magenta": "rose", "gray": "gris",
	} {
		design := GrokBotAvatarDesign("sparkle", source)
		if design == nil || design.Shape != "sparkle" || design.Color != color {
			t.Errorf("%s mapped to %+v", source, design)
		}
	}
}

func TestGrokBotAvatarFromDefinitionJSON(t *testing.T) {
	raw := []byte(`{"prompt":"hi","avatarShape":"hex","avatarColor":"orange"}`)
	design := GrokBotAvatarFromDefinition(raw)
	if design == nil || design.Shape != "hexagone" || design.Color != "orange" {
		t.Fatalf("definition parse %+v", design)
	}
}
