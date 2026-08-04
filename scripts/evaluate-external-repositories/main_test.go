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
	manifest, err := framework.LoadBenchmarkManifest(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCatalogAgainstManifest(catalog, manifest); err != nil {
		t.Fatal(err)
	}
}
