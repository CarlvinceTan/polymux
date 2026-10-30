package skills

import (
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSkills(t *testing.T) {
	guide, err := Polymux()
	if err != nil {
		t.Fatal(err)
	}
	report, err := Report()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(guide, "name: polymux") {
		t.Fatal("polymux skill missing its name")
	}
	for _, marker := range []string{"gh issue create", "{{REPO}}", IssueRepo, "explicit confirmation in chat"} {
		if strings.Contains(guide, marker) {
			t.Fatalf("polymux skill must point at the report skill without filing instructions, found %q", marker)
		}
	}
	if !strings.Contains(report, "name: report") {
		t.Fatal("report skill missing its name")
	}
	if !strings.Contains(report, IssueRepo) || strings.Contains(report, "{{REPO}}") {
		t.Fatal("report skill must substitute the issue repository")
	}
	if !strings.Contains(report, "explicit confirmation in chat") {
		t.Fatal("report skill must require confirmation before filing")
	}
	if !strings.Contains(report, "[REDACTED]") {
		t.Fatal("report skill must include the redaction marker")
	}
	if !strings.Contains(report, "Never splice user text into the command string") || !strings.Contains(report, "gh issue create --repo "+IssueRepo) {
		t.Fatal("report skill must keep safe issue creation")
	}
	if !strings.Contains(report, "https://github.com/"+IssueRepo+"/issues/new?title=") {
		t.Fatal("report skill must keep the prefilled issue link")
	}
}

func TestFiles(t *testing.T) {
	var paths []string
	err := fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		paths = append(paths, path)
		body, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "issue-loop") {
			t.Errorf("developer skill embedded in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"html-cards/SKILL.md", "polymux/SKILL.md", "report/SKILL.md"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("embedded files = %v, want %v", paths, want)
	}
}

func TestInstallListsSkills(t *testing.T) {
	dir := t.TempDir()
	installed, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 3 {
		t.Fatalf("installed %d skills", len(installed))
	}
	for _, skill := range installed {
		body, err := os.ReadFile(skill.Path)
		if err != nil || skill.Name == "" || skill.Description == "" || !strings.Contains(string(body), "name: "+skill.Name) {
			t.Fatalf("skill %+v unreadable or undescribed: %v", skill, err)
		}
	}
}
