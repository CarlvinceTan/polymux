package agentruntime

import (
	"strings"
	"testing"
)

func TestDeviceRejectsShellHostExpressions(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"-oProxyCommand=bad", "cvmac;bad", "user@host\ncommand", "host with spaces"} {
		t.Run(host, func(t *testing.T) {
			if (Device{Name: "cvmac", Host: host}).Valid() {
				t.Fatal("unsafe host accepted")
			}
		})
	}
	for _, host := range []string{"cvmac", "user@cvmac.taila465a3.ts.net", "100.127.193.111"} {
		t.Run(host, func(t *testing.T) {
			if !(Device{Name: "cvmac", Host: host}).Valid() {
				t.Fatal("valid paired host rejected")
			}
		})
	}
}
func TestRemoteOutputIsBounded(t *testing.T) {
	t.Parallel()
	output := &boundedOutput{}
	data := []byte(strings.Repeat("x", 128<<10))
	n, err := output.Write(data)
	if err != nil || n != len(data) || len(output.String()) != 64<<10 {
		t.Fatal("remote output was not drained and bounded")
	}
}
