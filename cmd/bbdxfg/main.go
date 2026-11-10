// Command bbdxfg is a TUI for creating standard-compliant test files of an
// exact size.
package main

import (
	"os"

	"github.com/bromanodlx/bbdx_file_gen/internal/cli"
)

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr)) }
