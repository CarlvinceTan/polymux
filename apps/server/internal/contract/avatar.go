package contract

import (
	"encoding/json"
	"strings"
)

// AvatarDesignFromProfile maps an imported bot profile's avatarShape/avatarColor to Polymux AvatarDesign ids.
func AvatarDesignFromProfile(shape, color string) *AvatarDesign {
	shape = strings.TrimSpace(strings.ToLower(shape))
	color = strings.TrimSpace(strings.ToLower(color))
	if shape == "" && (color == "" || color == "-") {
		return nil
	}
	mappedShape := mapProfileShape(shape)
	mappedColor := mapProfileColor(color)
	if mappedShape == "" && mappedColor == "" {
		return nil
	}
	if mappedShape == "" {
		mappedShape = "cercle"
	}
	if mappedColor == "" {
		mappedColor = "encre"
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

func mapProfileShape(shape string) string {
	switch shape {
	case "blob":
		return "cercle"
	case "square", "sparkle", "clover", "heart", "flower", "tablet", "wedge", "house", "star":
		return shape
	case "teardrop":
		return "goutte"
	case "hex":
		return "hexagone"
	case "cloud":
		return "nuage"
	default:
		return ""
	}
}

func mapProfileColor(color string) string {
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
	case "brown":
		return "brun"
	case "red":
		return "rouge"
	case "green":
		return "vert"
	case "gray":
		return "gris"
	default:
		return ""
	}
}

func AvatarFromDefinition(definition json.RawMessage) *AvatarDesign {
	var raw struct {
		AvatarShape string `json:"avatarShape"`
		AvatarColor string `json:"avatarColor"`
	}
	if err := json.Unmarshal(definition, &raw); err != nil {
		return nil
	}
	return AvatarDesignFromProfile(raw.AvatarShape, raw.AvatarColor)
}
