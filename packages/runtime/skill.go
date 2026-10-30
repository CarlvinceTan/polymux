package runtime

import "strings"

// Skill describes a file the host makes available to an agent.
type Skill struct{ Name, Description, Path string }

// DescribeSkill reads a skill's name and description from its front matter.
func DescribeSkill(body string) Skill {
	var skill Skill
	front, ok := strings.CutPrefix(body, "---\n")
	if !ok {
		return skill
	}
	front, _, _ = strings.Cut(front, "\n---")
	for _, line := range strings.Split(front, "\n") {
		key, value, _ := strings.Cut(line, ":")
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			skill.Name = value
		case "description":
			skill.Description = value
		}
	}
	return skill
}
