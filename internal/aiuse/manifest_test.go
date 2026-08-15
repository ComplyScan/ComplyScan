package aiuse

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestLoadOptionalMissingReturnsEmptyManifest(t *testing.T) {
	manifest, exists, err := LoadOptional(filepath.Join(t.TempDir(), "missing.yml"))
	if err != nil {
		t.Fatalf("LoadOptional() error = %v", err)
	}
	if exists {
		t.Fatal("LoadOptional() exists = true, want false")
	}
	if !reflect.DeepEqual(manifest, NewManifest()) {
		t.Fatalf("LoadOptional() manifest = %#v, want %#v", manifest, NewManifest())
	}
}

func TestLoadOptionalStrictYAML(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "unsupported version",
			content: `version: 2
uses: []
`,
			want: "unsupported version 2",
		},
		{
			name: "unknown root field",
			content: `version: 1
uses: []
unexpected: true
`,
			want: "field unexpected not found",
		},
		{
			name: "unknown use field",
			content: `version: 1
uses:
  - id: chat
    name: Chat
    description: Chat assistant
    paths: [internal/chat/**]
    status: active
    review: {status: draft}
    legal-conclusion: compliant
`,
			want: "field legal-conclusion not found",
		},
		{
			name: "multiple documents",
			content: `version: 1
uses: []
---
version: 1
uses: []
`,
			want: "multiple YAML documents are not supported",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ai-uses.yml")
			if err := os.WriteFile(path, []byte(test.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, _, err := LoadOptional(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadOptional() error = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestManifestValidationRejectsUnsafeOrAmbiguousValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{
			name: "duplicate use IDs",
			mutate: func(manifest *Manifest) {
				manifest.Uses = append(manifest.Uses, manifest.Uses[0])
			},
			want: "duplicated",
		},
		{
			name: "duplicate paths",
			mutate: func(manifest *Manifest) {
				manifest.Uses[0].Paths = []string{"internal/chat/**", "internal/chat/**"}
			},
			want: "duplicate value",
		},
		{
			name: "parent traversal",
			mutate: func(manifest *Manifest) {
				manifest.Uses[0].Paths = []string{"../secret"}
			},
			want: "unsafe path segment",
		},
		{
			name: "negated path",
			mutate: func(manifest *Manifest) {
				manifest.Uses[0].Paths = []string{"!internal/chat/**"}
			},
			want: "positive and repository-relative",
		},
		{
			name: "embedded control character",
			mutate: func(manifest *Manifest) {
				manifest.Uses[0].Paths = []string{"internal/chat\x01/client.go"}
			},
			want: "no control characters",
		},
		{
			name: "bidirectional formatting character",
			mutate: func(manifest *Manifest) {
				manifest.Uses[0].Name = "chat\u202eassistant"
			},
			want: "unsafe formatting characters",
		},
		{
			name: "invalid review",
			mutate: func(manifest *Manifest) {
				manifest.Uses[0].Review = profile.ProfileReview{Status: profile.ReviewConfirmed}
			},
			want: "when status is confirmed",
		},
		{
			name: "draft reviewer control character",
			mutate: func(manifest *Manifest) {
				manifest.Uses[0].Review = profile.ProfileReview{Status: profile.ReviewDraft, ReviewedBy: "reviewer\x1b[31m"}
			},
			want: "review.reviewed-by must not contain control characters",
		},
		{
			name: "invalid suggestion fingerprint",
			mutate: func(manifest *Manifest) {
				manifest.Uses[0].SuggestionFingerprints = []string{"not-a-fingerprint"}
			},
			want: "must be a 64-character SHA-256 value",
		},
		{
			name: "suggestion fingerprint linked twice",
			mutate: func(manifest *Manifest) {
				fingerprint := strings.Repeat("a", 64)
				manifest.Uses[0].SuggestionFingerprints = []string{fingerprint}
				second := testUse("second", profile.ReviewDraft, StatusActive, "second/**")
				second.SuggestionFingerprints = []string{fingerprint}
				manifest.Uses = append(manifest.Uses, second)
			},
			want: "already linked",
		},
		{
			name: "suggestion fingerprint both linked and dismissed",
			mutate: func(manifest *Manifest) {
				fingerprint := strings.Repeat("a", 64)
				manifest.Uses[0].SuggestionFingerprints = []string{fingerprint}
				manifest.Dismissals = []Dismissal{{Fingerprint: fingerprint, Reason: "Not a product use"}}
			},
			want: "already linked",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := NewManifest()
			manifest.Uses = []Use{testUse("chat", profile.ReviewDraft, StatusActive, "internal/chat/**")}
			test.mutate(&manifest)
			if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestWriteRejectsDuplicatesBeforeNormalization(t *testing.T) {
	manifest := NewManifest()
	use := testUse("chat", profile.ReviewDraft, StatusActive, "internal/chat/**")
	use.Paths = append(use.Paths, use.Paths[0])
	manifest.Uses = []Use{use}

	err := Write(filepath.Join(t.TempDir(), "ai-uses.yml"), manifest)
	if err == nil || !strings.Contains(err.Error(), "duplicate value") {
		t.Fatalf("Write() error = %v, want duplicate-value error", err)
	}
}

func TestWriteLoadRoundTripIsAtomicAndPredictable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "ai-uses.yml")
	manifest := NewManifest()
	second := testUse("zeta", profile.ReviewDraft, StatusActive, "z/**", "a/**")
	second.SystemIDs = []string{"system-b", "system-a"}
	second.SuggestionFingerprints = []string{strings.Repeat("b", 64), strings.Repeat("a", 64)}
	manifest.Uses = []Use{
		second,
		testUse("alpha", profile.ReviewConfirmed, StatusRetired, "retired/**"),
	}

	if err := Write(path, manifest); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	loaded, exists, err := LoadOptional(path)
	if err != nil {
		t.Fatalf("LoadOptional() error = %v", err)
	}
	if !exists {
		t.Fatal("LoadOptional() exists = false, want true")
	}
	if got := []string{loaded.Uses[0].ID, loaded.Uses[1].ID}; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("loaded use order = %v", got)
	}
	if !reflect.DeepEqual(loaded.Uses[1].Paths, []string{"a/**", "z/**"}) {
		t.Fatalf("loaded paths = %v", loaded.Uses[1].Paths)
	}
	if !reflect.DeepEqual(loaded.Uses[1].SystemIDs, []string{"system-a", "system-b"}) {
		t.Fatalf("loaded system IDs = %v", loaded.Uses[1].SystemIDs)
	}
	if !reflect.DeepEqual(loaded.Uses[1].SuggestionFingerprints, []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}) {
		t.Fatalf("loaded suggestion fingerprints = %v", loaded.Uses[1].SuggestionFingerprints)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), "# Developer-confirmed AI-use groupings.\n") {
		t.Fatalf("manifest header missing from %q", content)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("manifest permissions = %o, want 644", got)
		}
	}

	loaded.Uses[0].Description = "Updated retired use"
	if err := Write(path, loaded); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	reloaded, _, err := LoadOptional(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Uses[0].Description != "Updated retired use" {
		t.Fatalf("second round trip description = %q", reloaded.Uses[0].Description)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("directory entries after atomic writes = %v", entryNames(entries))
	}
}

func TestManifestRefusesSymlinkTargetAndParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	root := t.TempDir()
	manifest := NewManifest()
	manifest.Uses = []Use{testUse("chat", profile.ReviewDraft, StatusActive, "internal/chat/**")}

	realTarget := filepath.Join(root, "real.yml")
	if err := os.WriteFile(realTarget, []byte("do not replace"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedTarget := filepath.Join(root, "linked.yml")
	if err := os.Symlink(realTarget, linkedTarget); err != nil {
		t.Fatal(err)
	}
	if err := Write(linkedTarget, manifest); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Write(symlink target) error = %v", err)
	}
	if _, _, err := LoadOptional(linkedTarget); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("LoadOptional(symlink target) error = %v", err)
	}
	content, err := os.ReadFile(realTarget)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "do not replace" {
		t.Fatalf("symlink target content = %q", content)
	}

	realDirectory := filepath.Join(root, "real-directory")
	if err := os.Mkdir(realDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedDirectory := filepath.Join(root, "linked-directory")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Fatal(err)
	}
	parentLinkedTarget := filepath.Join(linkedDirectory, "ai-uses.yml")
	if err := os.WriteFile(filepath.Join(realDirectory, "ai-uses.yml"), []byte("version: 1\nuses: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(parentLinkedTarget, manifest); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Write(symlink parent) error = %v", err)
	}
	if _, _, err := LoadOptional(parentLinkedTarget); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("LoadOptional(symlink parent) error = %v", err)
	}
	content, err = os.ReadFile(filepath.Join(realDirectory, "ai-uses.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "version: 1\nuses: []\n" {
		t.Fatalf("symlinked-parent target content = %q", content)
	}
}

func TestManifestAllowsSymlinkAboveImmediateParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	root := t.TempDir()
	realAncestor := filepath.Join(root, "real-ancestor")
	manifestDirectory := filepath.Join(realAncestor, ".complyscan")
	if err := os.MkdirAll(manifestDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedAncestor := filepath.Join(root, "linked-ancestor")
	if err := os.Symlink(realAncestor, linkedAncestor); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(linkedAncestor, ".complyscan", "ai-uses.yml")
	manifest := NewManifest()
	manifest.Uses = []Use{testUse("chat", profile.ReviewDraft, StatusActive, "internal/chat/**")}

	if err := Write(path, manifest); err != nil {
		t.Fatalf("Write() through higher ancestor symlink error = %v", err)
	}
	loaded, exists, err := LoadOptional(path)
	if err != nil {
		t.Fatalf("LoadOptional() through higher ancestor symlink error = %v", err)
	}
	if !exists || len(loaded.Uses) != 1 || loaded.Uses[0].ID != "chat" {
		t.Fatalf("loaded manifest = %#v, exists = %v", loaded, exists)
	}
}

func TestSuggestionFingerprintIgnoresModelIDAndEvidenceOrder(t *testing.T) {
	first := providers.RepositoryAIUse{
		ID: "model-generated-one", Name: " Chat assistant ", Purpose: "Answers   questions", Lifecycle: "Production",
		Evidence: []providers.RepositoryCitation{
			{Path: "internal/chat/service.go", Line: 20, Summary: "Sends prompts"},
			{Path: "internal/chat/http.go", Line: 10, Summary: "Exposes chat"},
		},
	}
	second := first
	second.ID = "completely-different-id"
	second.Name = "chat ASSISTANT"
	second.Evidence = []providers.RepositoryCitation{first.Evidence[1], first.Evidence[0]}

	firstFingerprint := SuggestionFingerprint(first)
	if secondFingerprint := SuggestionFingerprint(second); secondFingerprint != firstFingerprint {
		t.Fatalf("fingerprints differ by model ID/order: %q != %q", firstFingerprint, secondFingerprint)
	}
	second.Purpose = "Generates source code"
	if SuggestionFingerprint(second) == firstFingerprint {
		t.Fatal("fingerprint did not change when the grouping purpose changed")
	}

	manifest := NewManifest()
	manifest.Dismissals = []Dismissal{{Fingerprint: firstFingerprint, Reason: "Not a distinct product use"}}
	if !IsDismissed(manifest, first) {
		t.Fatal("IsDismissed() = false for matching fingerprint")
	}
	if IsDismissed(manifest, second) {
		t.Fatal("IsDismissed() = true after fingerprinted content changed")
	}
	manifest.Uses = []Use{testUse("chat", profile.ReviewConfirmed, StatusActive, "internal/chat/**")}
	manifest.Uses[0].SuggestionFingerprints = []string{firstFingerprint}
	if got := LinkedSuggestionUses(manifest, first); !reflect.DeepEqual(got, []string{"chat"}) {
		t.Fatalf("LinkedSuggestionUses() = %v", got)
	}
	if got := LinkedSuggestionUses(manifest, second); len(got) != 0 {
		t.Fatalf("changed suggestion linked unexpectedly: %v", got)
	}
}

func TestNextIDIsStableAndCollisionSafe(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{
		testUse("chat-assistant", profile.ReviewDraft, StatusActive, "chat/**"),
		testUse("chat-assistant-2", profile.ReviewDraft, StatusActive, "chat-two/**"),
	}
	if got := NextID(manifest, "Chat assistant"); got != "chat-assistant-3" {
		t.Fatalf("NextID() = %q, want chat-assistant-3", got)
	}
	if first, second := NextID(manifest, "New use"), NextID(manifest, "New use"); first != second || first != "new-use" {
		t.Fatalf("repeated NextID() = %q, %q", first, second)
	}
	if got := NextID(manifest, "!!!"); got != "ai-use" {
		t.Fatalf("NextID(empty slug) = %q, want ai-use", got)
	}
	longID := NextID(manifest, strings.Repeat("very long use name ", 10))
	if len(longID) > 64 || !manifestIDPattern.MatchString(longID) {
		t.Fatalf("NextID(long name) = %q", longID)
	}
}

func testUse(id string, reviewStatus profile.ReviewStatus, status RecordStatus, paths ...string) Use {
	review := profile.ProfileReview{Status: reviewStatus}
	if reviewStatus == profile.ReviewConfirmed {
		review.ReviewedBy = "A. Reviewer"
		review.ReviewedAt = "2026-08-15"
	}
	return Use{
		ID: id, Name: strings.ReplaceAll(id, "-", " "), Description: "Repository-owned AI use",
		Paths: paths, Status: status, Review: review,
	}
}

func entryNames(entries []os.DirEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name())
	}
	return result
}
