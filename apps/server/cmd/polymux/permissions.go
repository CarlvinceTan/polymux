package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/CarlvinceTan/polymux/apps/server/internal/privacy"
)

// permissions prints what this machine lets Polymux control. With "grant" it opens the macOS panes
// that are still missing, and on Linux prints the commands to run, since they need root.
func permissions(args []string) error {
	grant := len(args) > 0 && args[0] == "grant"
	if len(args) > 0 && !grant {
		return fmt.Errorf("usage: polymux permissions [grant]")
	}
	items := privacy.Items()
	if len(items) == 0 {
		fmt.Println("Nothing to grant on this platform.")
		return nil
	}
	privacy.Render(os.Stdout, items)
	if privacy.Missing(items) == 0 {
		return nil
	}
	if !grant {
		fmt.Println("\nRun `polymux permissions grant` to fix what is missing.")
		return nil
	}
	if runtime.GOOS == "darwin" {
		// macOS only lets the user grant these, in System Settings; the app guides that step.
		for _, item := range items {
			if !item.OK && len(item.Fix) > 0 {
				if err := exec.Command("/bin/sh", "-c", item.Fix[0]).Run(); err != nil {
					return err
				}
				fmt.Printf("\nOpened %s in System Settings. Drag Polymux Server into the list, or use the Polymux app's Grant button.\n", item.Name)
				return nil
			}
		}
	}
	return grantLinux(items)
}

// grantLinux runs the missing items' commands after the user agrees, since they need root.
func grantLinux(items []privacy.Item) error {
	var commands, notes []string
	for _, item := range items {
		if item.OK {
			continue
		}
		for _, step := range item.Fix {
			if privacy.Runnable(step) {
				commands = append(commands, step)
			} else {
				notes = append(notes, step)
			}
		}
	}
	if len(commands) > 0 {
		fmt.Println("\nThis will run, as you (sudo asks for your password):")
		for _, command := range commands {
			fmt.Println("  " + command)
		}
		fmt.Print("Continue? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			return fmt.Errorf("nothing was changed")
		}
		for _, command := range commands {
			cmd := exec.Command("/bin/sh", "-c", command)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("%q failed: %w", command, err)
			}
		}
	}
	for _, note := range notes {
		fmt.Println("Next: " + note)
	}
	return nil
}
