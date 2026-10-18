package skills

import (
	"embed"
	"fmt"
	"strings"
)

// IssueRepo is the target repo for user bug reports.
const IssueRepo = "CarlvinceTan/polymux"

//go:embed polymux/SKILL.md report/SKILL.md
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

func text(path string) (string, error) {
	body, err := files.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("skills: read %s: %w", path, err)
	}
	return strings.ReplaceAll(string(body), "{{REPO}}", IssueRepo), nil
}
