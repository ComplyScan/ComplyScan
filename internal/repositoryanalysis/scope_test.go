package repositoryanalysis

import (
	"context"
	"fmt"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestScopeChangedReviewIncludesChangedAndBoundedConnectedCodeOnly(t *testing.T) {
	full := discovery.Repository{Root: ".", Files: []discovery.File{
		{Path: "changed.go", Kind: discovery.KindSource, Content: []byte("package app\nfunc changed() { first() }\n")},
		{Path: "first.go", Kind: discovery.KindSource, Content: []byte("package app\nfunc first() { second() }\n")},
		{Path: "second.go", Kind: discovery.KindSource, Content: []byte("package app\nfunc second() {}\n")},
		{Path: "unrelated.go", Kind: discovery.KindSource, Content: []byte("package app\nfunc unrelated() {}\n")},
		{Path: "changed.yml", Kind: discovery.KindConfig, Content: []byte("model: example\n")},
		{Path: "changed.md", Kind: discovery.KindDocumentation, Content: []byte("not model code context\n")},
	}}
	changed := discovery.Repository{Root: ".", Files: []discovery.File{full.Files[0], full.Files[4], full.Files[5]}}

	scoped, coverage := ScopeChangedReview(full, changed)
	paths := repositoryPathSet(scoped)
	for _, wanted := range []string{"changed.go", "changed.yml", "first.go", "second.go"} {
		if !paths[wanted] {
			t.Errorf("changed review scope omitted %q: %#v", wanted, paths)
		}
	}
	for _, unwanted := range []string{"changed.md", "unrelated.go"} {
		if paths[unwanted] {
			t.Errorf("changed review scope included unrelated or ineligible %q: %#v", unwanted, paths)
		}
	}
	if coverage.RepositoryFiles != 6 || coverage.IncludedFiles != 4 || coverage.ChangedFilesIncluded != 2 || coverage.ConnectedFilesIncluded != 2 {
		t.Fatalf("unexpected changed review coverage: %#v", coverage)
	}
}

func TestScopeChangedReviewCapsConnectedFiles(t *testing.T) {
	full := discovery.Repository{Root: ".", Files: []discovery.File{{
		Path: "changed.go", Kind: discovery.KindSource, Content: []byte("package app\nfunc changed() { helper00(); helper01(); helper02(); helper03(); helper04(); helper05(); helper06(); helper07(); helper08(); helper09(); helper10(); helper11() }\n"),
	}}}
	for index := 0; index < 12; index++ {
		full.Files = append(full.Files, discovery.File{
			Path: fmt.Sprintf("helper%02d.go", index), Kind: discovery.KindSource,
			Content: []byte(fmt.Sprintf("package app\nfunc helper%02d() {}\n", index)),
		})
	}
	changed := discovery.Repository{Root: ".", Files: []discovery.File{full.Files[0]}}

	scoped, coverage := ScopeChangedReview(full, changed)
	if coverage.ConnectedFilesIncluded != ChangedReviewConnectedFileLimit || len(scoped.Files) != 1+ChangedReviewConnectedFileLimit {
		t.Fatalf("connected context was not capped: files=%d coverage=%#v", len(scoped.Files), coverage)
	}
	paths := repositoryPathSet(scoped)
	if !paths["helper00.go"] || !paths["helper07.go"] || paths["helper08.go"] {
		t.Fatalf("connected cap was not deterministic: %#v", paths)
	}
}

func TestChangedReviewProviderRequestNeverContainsUnrelatedRepositoryContext(t *testing.T) {
	full := discovery.Repository{Root: ".", Files: []discovery.File{
		{Path: "changed.py", Kind: discovery.KindSource, Content: []byte("from openai import OpenAI\nfrom guard import authorize\nclient = OpenAI()\ndef generate(value):\n    authorize(value)\n    return client.responses.create(model='test', input=value)\n")},
		{Path: "guard.py", Kind: discovery.KindSource, Content: []byte("def authorize(value):\n    return value\n")},
		{Path: "unrelated.py", Kind: discovery.KindSource, Content: []byte("def approve_response(value):\n    return value\n")},
	}}
	changed := discovery.Repository{Root: ".", Files: []discovery.File{full.Files[0]}}
	scoped, scope := ScopeChangedReview(full, changed)
	reviewer := &repositoryFollowUpReviewer{}

	result, err := Run(context.Background(), reviewer, scoped, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	for requestIndex, request := range reviewer.requests {
		paths := make(map[string]bool)
		for _, file := range request.Files {
			paths[file.Path] = true
		}
		if !paths["changed.py"] || !paths["guard.py"] || paths["unrelated.py"] {
			t.Fatalf("provider request %d crossed changed review boundary: %#v", requestIndex, paths)
		}
		for _, symbol := range request.Graph.Symbols {
			if symbol.Path == "unrelated.py" {
				t.Fatalf("provider graph leaked unrelated path in request %d: %#v", requestIndex, request.Graph)
			}
		}
		for _, repositoryImport := range request.Graph.Imports {
			if repositoryImport.Path == "unrelated.py" {
				t.Fatalf("provider graph leaked unrelated import in request %d: %#v", requestIndex, request.Graph)
			}
		}
		for _, relationship := range request.Graph.Relationships {
			if relationship.Path == "unrelated.py" {
				t.Fatalf("provider graph leaked unrelated relationship in request %d: %#v", requestIndex, request.Graph)
			}
		}
	}
	scope.Apply(&result)
	if result.Coverage.ReviewScope != providers.RepositoryReviewScopeChanged || result.Coverage.RepositoryFiles != 3 || result.Coverage.ScopeFiles != 2 || result.Coverage.ChangedFiles != 1 || result.Coverage.ConnectedFiles != 1 {
		t.Fatalf("changed review coverage was not recorded truthfully: %#v", result.Coverage)
	}
	if result.FollowUpExcerpts != 0 {
		t.Fatalf("follow-up escaped the scoped repository: %#v", result)
	}
}

func repositoryPathSet(repository discovery.Repository) map[string]bool {
	result := make(map[string]bool, len(repository.Files))
	for _, file := range repository.Files {
		result[file.Path] = true
	}
	return result
}
