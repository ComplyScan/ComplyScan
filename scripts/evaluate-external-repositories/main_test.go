package main

import (
	"path/filepath"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/framework"
)

func TestExternalSourceCatalogValidation(t *testing.T) {
	source := sourceRepository{
		ID: "example", Name: "Example", URL: "https://github.com/example/example.git",
		Revision: "0123456789abcdef0123456789abcdef01234567", License: "MIT", LicenseFile: "LICENSE",
	}
	catalog := sourceCatalog{SchemaVersion: 1, ReviewedAt: "2026-08-04", Repositories: []sourceRepository{source}}
	if err := catalog.validate(); err != nil {
		t.Fatal(err)
	}
	manifest := framework.BenchmarkManifest{Cases: []framework.BenchmarkCase{{ID: "example", Path: "example"}}}
	if err := validateCatalogAgainstManifest(catalog, manifest); err != nil {
		t.Fatal(err)
	}
	catalog.Repositories[0].License = "Apache-2.0"
	if err := catalog.validate(); err != nil {
		t.Fatalf("Apache-2.0 source was rejected: %v", err)
	}
	catalog.Repositories[0].License = "GPL-3.0"
	if err := catalog.validate(); err == nil {
		t.Fatal("unsupported licence was accepted")
	}
	catalog.Repositories[0].License = "MIT"
	catalog.Repositories[0].LicenseFile = "../LICENSE"
	if err := catalog.validate(); err == nil {
		t.Fatal("unsafe licence path was accepted")
	}
}

func TestCheckedInExternalStudyMetadata(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "technical-evaluation", "external")
	catalog, err := loadSourceCatalog(filepath.Join(root, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", "nist-manifest.json"} {
		manifest, err := framework.LoadBenchmarkManifest(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		if err := validateCatalogAgainstManifest(catalog, manifest); err != nil {
			t.Fatalf("validate %s: %v", name, err)
		}
		if name == "nist-manifest.json" && benchmarkCandidateCount(manifest) != 32 {
			t.Fatalf("NIST reviewed candidate count = %d, want 32", benchmarkCandidateCount(manifest))
		}
	}
	if _, err := loadSemanticBenchmarkConfig(filepath.Join(root, "semantic.json")); err != nil {
		t.Fatal(err)
	}
}

func benchmarkCandidateCount(manifest framework.BenchmarkManifest) int {
	total := 0
	for _, benchmarkCase := range manifest.Cases {
		total += len(benchmarkCase.ExpectedCandidates)
	}
	return total
}
