package privacy

import (
	"os"
	"os/exec"
	"os/user"
)

// Items reports what this Linux session lets Polymux see and control.
func Items() []Item {
	return []Item{screenItem(), inputItem()}
}

func session() string {
	if kind := os.Getenv("XDG_SESSION_TYPE"); kind != "" {
		return kind
	}
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "":
		return "wayland"
	case os.Getenv("DISPLAY") != "":
		return "x11"
	}
	return ""
}

func screenItem() Item {
	switch session() {
	case "x11":
		return Item{Name: "Screen capture", OK: true, Detail: "X11 session; windows and the screen can be read"}
	case "wayland":
		// Wayland asks the user through the desktop portal, once per app; nothing can pre-grant it.
		ok := exec.Command("pgrep", "-x", "xdg-desktop-portal").Run() == nil
		item := Item{Name: "Screen capture", OK: ok, Detail: "Wayland session; the desktop portal asks the first time"}
		if !ok {
			item.Fix = []string{"install and start xdg-desktop-portal with the backend for your desktop"}
		}
		return item
	}
	return Item{Name: "Screen capture", Detail: "no graphical session; run Polymux inside a desktop session", Fix: []string{"log in to a desktop, or start an X11 session"}}
}

func inputItem() Item {
	file, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err == nil {
		file.Close()
		return Item{Name: "Keyboard and mouse", OK: true, Detail: "/dev/uinput can be opened"}
	}
	name := "USER"
	if current, err := user.Current(); err == nil {
		name = current.Username
	}
	return Item{Name: "Keyboard and mouse", Detail: "/dev/uinput can't be opened by " + name, Fix: UinputSteps(name)}
}
