package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "polymux: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Println("usage: polymux serve [server flags] | pair [--url URL] [--no-qr] | export FILE | import FILE [--force] | version")
		return nil
	}
	if args[0] == "version" {
		fmt.Println(buildSHA)
		return nil
	}
	if args[0] == "pair" {
		return pair(args[1:])
	}
	if args[0] == "export" || args[0] == "import" {
		if len(args) < 2 {
			return fmt.Errorf("usage: polymux %s FILE [--force]", args[0])
		}
		force := len(args) > 2 && args[2] == "--force"
		if args[0] == "export" {
			return exportData(args[1])
		}
		return importData(args[1], force)
	}
	if args[0] != "serve" && args[0] != "server" {
		return fmt.Errorf("unknown command %q", args[0])
	}
	return runServer(args[1:])
}
