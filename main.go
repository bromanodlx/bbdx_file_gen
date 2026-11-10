// Command bbdx_file_gen is a TUI for creating standard-compliant test files.
// The same tool is also available under the short name bbdxfg (cmd/bbdxfg).
package main

import (
	"os"

	"bbdx_file_gen/internal/cli"
)

func main() { os.Exit(cli.Run("bbdx_file_gen", os.Args[1:], os.Stdout, os.Stderr)) }
