// Command bbdxfg is the short name of bbdx_file_gen, a TUI for creating
// standard-compliant test files. Both commands behave identically.
package main

import (
	"os"

	"bbdx_file_gen/internal/cli"
)

func main() { os.Exit(cli.Run("bbdxfg", os.Args[1:], os.Stdout, os.Stderr)) }
