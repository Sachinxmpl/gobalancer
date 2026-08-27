package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "loadgate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		usage(os.Stderr)
		return errors.New("no command given")
	}

	switch args[1] {
	case "check":
		return Check(args[2:])
	case "run":
		return Serve(args[2:])
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func usage(w io.Writer) {
	sample := `
loadgate - L4/L7 load balancer

Build:
	make build

Usage:
  ./bin/loadgate check -c <file>   validate a config file and exit
  ./bin/loadgate run   -c <file>   serve until interrupted

Flags:
  -c string             path to config file (default "config.yaml")
  -log-level string     debug, info, warn or error (default "info")
  -log-format string    json or text (default "json")
`
	_, _ = io.WriteString(w, sample)
}
