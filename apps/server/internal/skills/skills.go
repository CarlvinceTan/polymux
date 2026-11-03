package skills

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IssueRepo is the target repo for user bug reports.
const IssueRepo = "CarlvinceTan/polymux"

// CardsName identifies the card reference among installed files. It ships with show_html, so it isn't a listed skill.
const CardsName = "html-cards"

const cardsFile = CardsName + "/REFERENCE.md"

//go:embed polymux/SKILL.md report/SKILL.md skill-creator/SKILL.md skill-creator/LICENSE.txt plugin-creator/SKILL.md html-cards/REFERENCE.md
var files embed.FS

// Polymux returns the bundled guide to Polymux itself.
func Polymux() (string, error) {
	return text("polymux/SKILL.md")
}

// Report returns the bundled bug-report and feedback skill.
func Report() (string, error) {
	return text("report/SKILL.md")
}

// Bundled returns the Polymux guide and the report skill, in that order.
func Bundled() (string, error) {
	guide, err := Polymux()
	if err != nil {
		return "", err
	}
	report, err := Report()
	if err != nil {
		return "", err
	}
	return guide + "\n" + report, nil
}

// Cards returns the reference for building show_html cards.
func Cards() (string, error) {
	return text(cardsFile)
}

func text(path string) (string, error) {
	body, err := files.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("skills: read %s: %w", path, err)
	}
	return strings.ReplaceAll(string(body), "{{REPO}}", IssueRepo), nil
}

// Skill is one skill as a prompt lists it: what it is for and where its full text is.
type Skill struct {
	Name, Description, Path string
}

// Names lists the bundled skills in their prompt order.
func Names() []string {
	return []string{"polymux", "report", "skill-creator", "plugin-creator"}
}

// Install writes the bundled skills and the card reference under dir, so an agent can read one
// when it needs it instead of carrying every skill's text in each request.
func Install(dir string) ([]Skill, error) {
	var out []Skill
	for _, name := range Names() {
		body, path, err := install(dir, name+"/SKILL.md")
		if err != nil {
			return nil, err
		}
		skill := Describe(body)
		skill.Path = path
		out = append(out, skill)
	}
	if _, _, err := install(dir, "skill-creator/LICENSE.txt"); err != nil {
		return nil, err
	}
	_, path, err := install(dir, cardsFile)
	if err != nil {
		return nil, err
	}
	return append(out, Skill{Name: CardsName, Path: path}), nil
}

func install(dir, file string) (body, path string, err error) {
	if body, err = text(file); err != nil {
		return "", "", err
	}
	path = filepath.Join(dir, file)
	if current, err := os.ReadFile(path); err != nil || string(current) != body {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", "", err
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return "", "", err
		}
	}
	return body, path, nil
}

// Describe reads a skill's name and description from its front matter.
func Describe(body string) Skill {
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
