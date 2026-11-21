package hub

import (
	"html"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

func mentionRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsNumber(value) || strings.ContainsRune("_.-", value)
}

// mentionPlaces finds standalone uses of a label. One inside an email address or a longer handle isn't a mention.
func mentionPlaces(body, label string) []int {
	out := []int{}
	if label == "" {
		return out
	}
	for from := 0; from < len(body); {
		index := strings.Index(body[from:], label)
		if index < 0 {
			break
		}
		start := from + index
		end := start + len(label)
		before, _ := utf8.DecodeLastRuneInString(body[:start])
		after, _ := utf8.DecodeRuneInString(body[end:])
		if (start == 0 || !mentionRune(before)) && (end == len(body) || !mentionRune(after)) {
			out = append(out, start)
		}
		from = end
	}
	return out
}

func textHTML(value string) string {
	return strings.ReplaceAll(html.EscapeString(value), "\n", "<br>")
}

// mentionHTML links only the labels sent with an identity; everything typed stays text.
func mentionHTML(body string, users []contract.HubMention) string {
	type place struct {
		start, end int
		id         string
	}
	places := []place{}
	for _, user := range users {
		for _, start := range mentionPlaces(body, user.Label) {
			places = append(places, place{start, start + len(user.Label), user.ID})
		}
	}
	slices.SortFunc(places, func(a, b place) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return b.end - a.end
	})
	var output strings.Builder
	cursor := 0
	for _, item := range places {
		if item.start < cursor {
			continue
		}
		output.WriteString(textHTML(body[cursor:item.start]))
		output.WriteString(`<a href="https://matrix.to/#/` + html.EscapeString(item.id) + `">` + html.EscapeString(body[item.start:item.end]) + `</a>`)
		cursor = item.end
	}
	output.WriteString(textHTML(body[cursor:]))
	return output.String()
}

// mentionContent adds what lets a bridge make a native mention, for the labels still in the text.
func mentionContent(body string, mentions *contract.HubMentions) map[string]any {
	if mentions == nil {
		return nil
	}
	users := []contract.HubMention{}
	ids := []string{}
	for _, user := range mentions.Users {
		if !strings.HasPrefix(user.ID, "@") || !strings.HasPrefix(user.Label, "@") || len(mentionPlaces(body, user.Label)) == 0 {
			continue
		}
		users = append(users, user)
		if !slices.Contains(ids, user.ID) {
			ids = append(ids, user.ID)
		}
	}
	everyone := mentions.Everyone && len(mentionPlaces(body, "@everyone")) > 0
	if len(ids) == 0 && !everyone {
		return nil
	}
	targets := map[string]any{}
	if len(ids) > 0 {
		targets["user_ids"] = ids
	}
	if everyone {
		targets["room"] = true
	}
	return map[string]any{"format": "org.matrix.custom.html", "formatted_body": mentionHTML(body, users), "m.mentions": targets}
}
