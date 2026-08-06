package main

import (
	"os"

	"github.com/ComplyScan/ComplyScan/internal/cli"
)

var (
	version   = "0.1.3-dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr, cli.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	}))
}
