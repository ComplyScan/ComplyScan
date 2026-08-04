package reviewcontext

import (
	"fmt"
	"strings"
	"testing"
)

func TestBoundedRepositoryExcerptIncludesWiderMatchedFileContext(t *testing.T) {
	var content strings.Builder
	for line := 1; line <= 180; line++ {
		fmt.Fprintf(&content, "line-%03d\n", line)
	}
	excerpt := boundedRepositoryExcerpt([]byte(content.String()), 90, 20_000)
	if !strings.Contains(excerpt, "line-031") || !strings.Contains(excerpt, "line-149") {
		t.Fatalf("matched-file context did not include the bounded 60-line window: %s", excerpt)
	}
	if strings.Contains(excerpt, "line-001") || strings.Contains(excerpt, "line-180") {
		t.Fatalf("matched-file context escaped its bounded window: %s", excerpt)
	}
	if got := boundedRepositoryExcerpt([]byte(content.String()), 90, 80); len([]rune(got)) > 80 {
		t.Fatalf("character cap was exceeded: %d", len([]rune(got)))
	}
}
