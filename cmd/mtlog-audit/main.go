// Package main provides the mtlog-audit CLI tool.
package main

import (
	"fmt"
	"os"

	"github.com/willibrandon/mtlog-audit/cmd/mtlog-audit/commands"
)

var version = "dev"

func main() {
	if err := commands.Execute(version); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
