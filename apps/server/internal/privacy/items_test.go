package privacy

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderShowsFixesOnlyForMissingItems(t *testing.T) {
	var out bytes.Buffer
	Render(&out, []Item{
		{Name: "Screen capture", OK: true, Detail: "fine", Fix: []string{"never shown"}},
		{Name: "Keyboard and mouse", Detail: "no access", Fix: []string{"sudo usermod -aG input me"}},
	})
	text := out.String()
	if strings.Contains(text, "never shown") || !strings.Contains(text, "sudo usermod -aG input me") {
		t.Fatalf("fixes should appear only for missing items:\n%s", text)
	}
	if strings.Count(text, "missing") != 1 || strings.Count(text, "granted") != 1 {
		t.Fatalf("states:\n%s", text)
	}
}

func TestMissingCountsUngrantedItems(t *testing.T) {
	if Missing([]Item{{OK: true}, {}, {}}) != 2 {
		t.Fatal("Missing should count the items that are not granted")
	}
}

func TestUinputStepsGiveTheUserAccessAndAskForARelogin(t *testing.T) {
	steps := strings.Join(UinputSteps("ada"), "\n")
	for _, want := range []string{"60-polymux-uinput.rules", "modprobe uinput", "usermod -aG input ada", "log out"} {
		if !strings.Contains(steps, want) {
			t.Fatalf("missing %q in:\n%s", want, steps)
		}
	}
}

func TestOnlyCommandsRun(t *testing.T) {
	for _, step := range UinputSteps("ada") {
		if strings.Contains(step, "log out") == Runnable(step) {
			t.Fatalf("%q: advice must not run, commands must", step)
		}
	}
}

func TestUinputRuleFileEndsInANewline(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rule")
	step := strings.Replace(UinputSteps("ada")[0], "sudo tee /etc/udev/rules.d/60-polymux-uinput.rules", "tee "+file, 1)
	if out, err := exec.Command("/bin/sh", "-c", step).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data, err := os.ReadFile(file)
	if err != nil || !strings.HasPrefix(string(data), "KERNEL==") || !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("rule file = %q, %v", data, err)
	}
}
