package privacy

import (
	"fmt"
	"io"
	"strings"
)

// Item is one permission this machine can give Polymux, and how to fix it when it is missing.
type Item struct {
	Name   string
	OK     bool
	Detail string
	// Fix is what to run or open, one step per line.
	Fix []string
}

// Render writes the items as a table the CLI prints.
func Render(w io.Writer, items []Item) {
	for _, item := range items {
		state := "granted"
		if !item.OK {
			state = "missing"
		}
		fmt.Fprintf(w, "%-24s %-8s %s\n", item.Name, state, item.Detail)
		if !item.OK {
			for _, step := range item.Fix {
				fmt.Fprintf(w, "    %s\n", step)
			}
		}
	}
}

// Missing counts the items that still need granting.
func Missing(items []Item) int {
	n := 0
	for _, item := range items {
		if !item.OK {
			n++
		}
	}
	return n
}

// uinputRule lets the input group open /dev/uinput, which is how a Linux process types and clicks.
const uinputRule = `KERNEL=="uinput", GROUP="input", MODE="0660", OPTIONS+="static_node=uinput"` + "\n"

// UinputSteps is what gives a user input injection on Linux, as commands for a root shell.
func UinputSteps(user string) []string {
	return []string{
		"printf '%s\\n' '" + strings.TrimSpace(uinputRule) + "' | sudo tee /etc/udev/rules.d/60-polymux-uinput.rules",
		"sudo modprobe uinput",
		"sudo udevadm control --reload-rules && sudo udevadm trigger",
		"sudo usermod -aG input " + user,
		"then log out and back in so the group applies",
	}
}

// Runnable is whether a fix step is a command, rather than advice the user acts on.
func Runnable(step string) bool {
	return strings.HasPrefix(step, "sudo ") || strings.HasPrefix(step, "printf ")
}
