package main

import (
	"fmt"
	"os"
)

var version = "1.1.0"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, verbUsage)
		os.Exit(1)
	}

	cmd := os.Args[1]
	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		fmt.Fprintln(os.Stdout, verbUsage)
		os.Exit(0)
	}
	if cmd == "-V" || cmd == "-v" || cmd == "--version" || cmd == "version" {
		fmt.Printf("ting %s\n", version)
		os.Exit(0)
	}

	if run, ok := verbs[cmd]; ok {
		os.Exit(runVerb(run, os.Args[2:]))
	}

	fmt.Fprintf(os.Stderr, "ting: unknown command %q\n\n%s\n", cmd, verbUsage)
	os.Exit(1)
}
