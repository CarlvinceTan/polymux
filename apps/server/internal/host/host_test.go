package host

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestUnitHasNoShellOrCredentialEnvironment(t *testing.T) {
	t.Parallel()
	data, err := Unit(Label, "/Applications/A & B.app/Contents/Helpers/polymux", "/Users/user/.polymux/logs/launchd.log", nil)
	if err != nil {
		t.Fatal(err)
	}
	var document struct{ XMLName xml.Name }
	if xml.Unmarshal(data, &document) != nil || document.XMLName.Local != "plist" {
		t.Fatal("invalid plist")
	}
	text := string(data)
	if strings.Contains(text, "EnvironmentVariables") || strings.Contains(text, "/bin/sh") || !strings.Contains(text, "A &amp; B.app") {
		t.Fatalf("unsafe service unit: %s", text)
	}
}

func TestUnitPassesOnlyInstanceSettings(t *testing.T) {
	t.Setenv("POLYMUX_DATA_DIR", "/tmp/instance/data")
	t.Setenv("POLYMUX_TOKEN_FILE", "/tmp/secret")
	data, err := Unit("test.label", "/tmp/polymux", "/tmp/launchd.log", environment())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "/tmp/instance/data") || strings.Contains(text, "/tmp/secret") {
		t.Fatalf("environment: %s", text)
	}
}

func TestUnitRefusesRelativePaths(t *testing.T) {
	t.Parallel()
	if _, err := Unit(Label, "polymux", "/tmp/launchd.log", nil); err == nil {
		t.Fatal("a relative binary would run whatever launchd finds")
	}
}
