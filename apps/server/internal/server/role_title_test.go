package server

import "testing"

func TestRoleTitle(t *testing.T) {
	for input, want := range map[string]string{
		"polymux maintainer":  "Polymux Maintainer",
		"Polymux Maintainer":  "Polymux Maintainer",
		"head of design":      "Head of Design",
		"  travel   planner ": "Travel Planner",
		"iOS developer":       "iOS Developer",
		"the planner":         "The Planner",
		"":                    "",
	} {
		if got := roleTitle(input); got != want {
			t.Errorf("roleTitle(%q) = %q, want %q", input, got, want)
		}
	}
}
