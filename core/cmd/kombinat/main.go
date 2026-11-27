// Command kombinat is the Kombinat MkII core: one binary that loads,
// configures and serves modules. Every subcommand lives in package cli.
package main

import (
	"os"

	"github.com/Szmelc-INC/Kombinat/core/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.IO{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
		TTY:    isTerminal(os.Stdout),
	}))
}

// isTerminal reports whether f is a character device (an interactive terminal).
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
