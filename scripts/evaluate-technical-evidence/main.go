package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ComplyScan/ComplyScan/internal/framework"
)

func main() {
	os.Exit(run())
}

func run() int {
	manifestPath := flag.String("manifest", "testdata/technical-evaluation/manifest.json", "path to the versioned benchmark manifest")
	format := flag.String("format", "text", "output format: text or json")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Usage: evaluate-technical-evidence [--manifest PATH] [--format text|json]")
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(os.Stderr, "Error: unsupported format %q (choose text or json)\n", *format)
		return 2
	}
	resolvedManifest, err := filepath.Abs(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: resolve manifest: %v\n", err)
		return 2
	}
	manifest, err := framework.LoadBenchmarkManifest(resolvedManifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	pack, err := framework.LoadBuiltin(manifest.PackID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	report, err := framework.RunBenchmark(context.Background(), resolvedManifest, manifest, pack)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	if *format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "Error: write JSON result: %v\n", err)
			return 2
		}
	} else if err := framework.WriteBenchmarkSummary(os.Stdout, report); err != nil {
		fmt.Fprintf(os.Stderr, "Error: write benchmark summary: %v\n", err)
		return 2
	}
	if !report.Passed {
		return 1
	}
	return 0
}
