package main

import (
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/host"
)

func TestHostAddressFollowsInstance(t *testing.T) {
	if address := hostAddress(); address != "http://127.0.0.1:7337" {
		t.Fatalf("default address = %q", address)
	}
	t.Setenv("POLYMUX_LISTEN", "127.0.0.1:8000")
	if address := hostAddress(); address != "http://127.0.0.1:8000" {
		t.Fatalf("instance address = %q", address)
	}
}

func TestTestInstanceNeverNamesTheRealJob(t *testing.T) {
	t.Setenv("POLYMUX_HOST_LABEL", "com.flarehq.polymux.server.test.1")
	t.Setenv("POLYMUX_HOST_DIR", t.TempDir())
	service, err := host.Open()
	if err != nil {
		t.Fatal(err)
	}
	if service.Label == host.Label || strings.Contains(service.Path, "Library/LaunchAgents") {
		t.Fatalf("service %+v", service)
	}
}
