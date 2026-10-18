package contract

import (
	"encoding/json"
	"strings"
)

// GrokBotAvatarDesign maps Grok Bot profile.json avatarShape/avatarColor to Polymux AvatarDesign ids.
func GrokBotAvatarDesign(shape, color string) *AvatarDesign {
	shape = strings.TrimSpace(strings.ToLower(shape))
	color = strings.TrimSpace(strings.ToLower(color))
	if shape == "" && (color == "" || color == "-") {
		return nil
	}
	mappedShape := mapGrokShape(shape)
	mappedColor := mapGrokColor(color)
	if mappedShape == "" && mappedColor == "" {
		return nil
	}
	if mappedShape == "" {
		mappedShape = "cercle"
	}
	if mappedColor == "" {
		mappedColor = "encre" // TODO(avatar-defaults): match Grok Bot sidebar when specs/avatar-defaults.md lands
	}
	return &AvatarDesign{
		Shape:        mappedShape,
		Color:        mappedColor,
		Expression:   "neutre",
		FollowCursor: true,
		Animates:     true,
		Blocks:       []AnimationBlock{},
	}
}

func mapGrokShape(shape string) string {
	switch shape {
	case "blob":
		return "galet"
	case "wedge":
		return "triangle"
	case "square":
		return "squircle"
	case "hex":
		return "hexagone"
	case "cloud":
		return "nuage"
	default:
		return ""
	}
}

func mapGrokColor(color string) string {
	if color == "" || color == "-" {
		return ""
	}
	switch color {
	case "violet":
		return "violet"
	case "cyan":
		return "turquoise"
	case "magenta":
		return "rose"
	case "yellow":
		return "ambre"
	case "blue":
		return "bleu"
	case "orange":
		return "orange"
	case "black":
		return "encre"
	default:
		return ""
	}
}

func GrokBotAvatarFromDefinition(definition json.RawMessage) *AvatarDesign {
	var raw struct {
		AvatarShape string `json:"avatarShape"`
		AvatarColor string `json:"avatarColor"`
	}
	if err := json.Unmarshal(definition, &raw); err != nil {
		return nil
	}
	return GrokBotAvatarDesign(raw.AvatarShape, raw.AvatarColor)
}
