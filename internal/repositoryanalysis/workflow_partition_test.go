package repositoryanalysis

import (
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestPartitionTargetedRepositoryKeepsCrossDirectoryWorkflowTogether(t *testing.T) {
	paths := []string{"api/handler.go", "providers/client.go", "unrelated/catalog.go"}
	repository := discovery.Repository{Root: "."}
	files := make([]providers.RepositorySourceFile, 0, len(paths))
	for _, path := range paths {
		content := strings.Repeat("x", 1_450)
		repository.Files = append(repository.Files, discovery.File{
			Path: path, Kind: discovery.KindSource, Content: []byte(content), Size: int64(len(content)),
		})
		files = append(files, providers.RepositorySourceFile{
			Path: path, Kind: string(discovery.KindSource), Content: content, ContentStartLine: 1, LineCount: 1,
		})
	}
	graph := codegraph.Graph{
		Symbols: []codegraph.Symbol{
			{ID: "handler", Path: paths[0], StartLine: 1, EndLine: 1},
			{ID: "client", Path: paths[1], StartLine: 1, EndLine: 1},
			{ID: "catalog", Path: paths[2], StartLine: 1, EndLine: 1},
		},
		Edges: []codegraph.Edge{{Kind: codegraph.EdgeCall, From: "handler", To: "client", Resolved: true}},
	}

	chunks, err := partitionTargetedRepository(repository, graph, files, nil, 3_300, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("targeted workflow partition produced %d chunks, want 2: %#v", len(chunks), chunks)
	}
	if got := chunkPaths(chunks[0]); len(got) != 2 || got[0] != paths[0] || got[1] != paths[1] {
		t.Fatalf("connected cross-directory evidence = %v, want %v together", got, paths[:2])
	}
	if got := chunkPaths(chunks[1]); len(got) != 1 || got[0] != paths[2] {
		t.Fatalf("unrelated evidence bundle = %v, want [%s]", got, paths[2])
	}
}

func chunkPaths(chunk repositoryChunk) []string {
	paths := make([]string, 0, len(chunk.files))
	for _, file := range chunk.files {
		paths = append(paths, file.Path)
	}
	return paths
}
