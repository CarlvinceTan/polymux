package main

import "testing"

func TestHostDefaults(t *testing.T) {
	for _, mode := range []string{"run", "install"} {
		t.Run(mode, func(t *testing.T) {
			listen, control, err := hostListeners("", "", false)
			if err != nil || listen != "127.0.0.1:7337" || control != "127.0.0.1:7338" {
				t.Fatalf("host listeners = %q, %q, %v", listen, control, err)
			}
		})
	}
}

func TestHostListeners(t *testing.T) {
	for _, test := range []struct {
		name, listen, control, want string
		invalid                     bool
	}{
		{name: "adjacent port", listen: "192.168.1.5:8000", want: "127.0.0.1:8001"},
		{name: "explicit control", listen: "127.0.0.1:8000", control: "127.0.0.1:9000", want: "127.0.0.1:9000"},
		{name: "no adjacent port", listen: "127.0.0.1:65535", invalid: true},
		{name: "invalid port", listen: "127.0.0.1:bad", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, control, err := hostListeners(test.listen, test.control, false)
			if (err != nil) != test.invalid {
				t.Fatalf("error = %v", err)
			}
			if err == nil && control != test.want {
				t.Errorf("controller = %q, want %q", control, test.want)
			}
		})
	}
}
