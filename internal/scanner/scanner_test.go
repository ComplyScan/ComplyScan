package scanner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

type bufferedTestRule struct{}

func (bufferedTestRule) ID() string { return "TEST-001" }

func (bufferedTestRule) Run(context.Context, discovery.Repository) ([]rules.Finding, error) {
	return []rules.Finding{{RuleID: "TEST-001", Severity: rules.SeverityInfo}}, nil
}

func TestScannerRunsOfflineRulePipeline(t *testing.T) {
	target := filepath.Join("..", "..", "testdata", "documented-ai-app")
	result, err := New().Scan(context.Background(), target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) == 0 {
		t.Fatal("expected AI inventory finding")
	}
	for _, finding := range result.Findings {
		if len(finding.Fingerprint) != 64 {
			t.Fatalf("finding has invalid fingerprint: %#v", finding)
		}
		if finding.RuleID == "AI-DOC-001" || finding.RuleID == "AI-RISK-001" {
			t.Fatalf("documented fixture produced missing-evidence finding: %#v", finding)
		}
	}
}

func TestScannerRunsAgainstPreDiscoveredRepository(t *testing.T) {
	target := t.TempDir()
	discovered := discovery.Result{Repository: discovery.Repository{Root: target, Files: []discovery.File{{
		Path: "app.py", Kind: discovery.KindSource, Content: []byte("from openai import OpenAI\nclient = OpenAI()\n"),
	}}}}
	result, err := New().ScanDiscovered(context.Background(), target, discovered, Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range result.Findings {
		if finding.RuleID == "AI-DISC-001" && finding.Path == "app.py" {
			found = true
		}
	}
	if !found {
		t.Fatalf("pre-discovered findings = %#v", result.Findings)
	}
}

func TestFindingFingerprintSurvivesLineMovement(t *testing.T) {
	first := rules.Finding{RuleID: "TEST-001", Title: "Test", Path: "src/app.go", StartLine: 4, Evidence: "logger.Info(prompt)"}
	second := first
	second.StartLine = 40
	if rules.ComputeFingerprint(first) != rules.ComputeFingerprint(second) {
		t.Fatal("line movement changed fingerprint")
	}
	second.Evidence = "logger.Info(response)"
	if rules.ComputeFingerprint(first) == rules.ComputeFingerprint(second) {
		t.Fatal("different evidence produced the same fingerprint")
	}
}

func TestFindingScopeClassifiesRepositoryEvidence(t *testing.T) {
	tests := []struct {
		path string
		kind discovery.FileKind
		want rules.FindingScope
	}{
		{path: "src/app.py", kind: discovery.KindSource, want: rules.ScopeProduction},
		{path: "tests/test_app.py", kind: discovery.KindSource, want: rules.ScopeTest},
		{path: "docs/example.md", kind: discovery.KindDocumentation, want: rules.ScopeDocumentation},
		{path: "pyproject.toml", kind: discovery.KindManifest, want: rules.ScopeConfiguration},
	}
	for _, test := range tests {
		if actual := findingScope(test.path, test.kind); actual != test.want {
			t.Errorf("findingScope(%q, %q) = %q, want %q", test.path, test.kind, actual, test.want)
		}
	}
}

func TestScannerStreamsEveryFinding(t *testing.T) {
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	var streamed []rules.Finding
	result, err := New().Scan(context.Background(), target, Options{
		OnFinding: func(finding rules.Finding) error {
			streamed = append(streamed, finding)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(streamed) == 0 {
		t.Fatal("expected findings to be streamed")
	}
	if len(streamed) != len(result.Findings) {
		t.Fatalf("streamed %d findings, final result has %d", len(streamed), len(result.Findings))
	}
}

func TestScannerStreamsFindingsFromBufferedExtensionRules(t *testing.T) {
	target := filepath.Join("..", "..", "testdata", "non-ai-repository")
	streamed := 0
	result, err := New(bufferedTestRule{}).Scan(context.Background(), target, Options{
		OnFinding: func(rules.Finding) error {
			streamed++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if streamed != 1 || len(result.Findings) != 1 {
		t.Fatalf("streamed=%d findings=%d, want 1 and 1", streamed, len(result.Findings))
	}
}

func TestScannerCanDisableRule(t *testing.T) {
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	result, err := New().Scan(context.Background(), target, Options{
		RuleEnabled: func(id string) bool { return id != "AI-LOG-001" },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range result.Findings {
		if finding.RuleID == "AI-LOG-001" {
			t.Fatalf("disabled rule produced finding: %#v", finding)
		}
	}
}

func TestScannerSuppressesBeforeStreaming(t *testing.T) {
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	streamed := 0
	result, err := New().Scan(context.Background(), target, Options{
		Suppress: func(finding rules.Finding) bool { return finding.RuleID == "AI-LOG-001" },
		OnFinding: func(rules.Finding) error {
			streamed++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Suppressed == 0 {
		t.Fatal("expected a suppressed finding")
	}
	if streamed != len(result.Findings) {
		t.Fatalf("streamed=%d findings=%d", streamed, len(result.Findings))
	}
	for _, finding := range result.Findings {
		if finding.RuleID == "AI-LOG-001" {
			t.Fatal("suppressed finding remained in result")
		}
	}
}

func TestChangedSinceScopesCodeRulesButKeepsGovernanceRepositoryWide(t *testing.T) {
	target := t.TempDir()
	runScannerGit(t, target, "init", "-q")
	runScannerGit(t, target, "config", "user.name", "ComplyScan tests")
	runScannerGit(t, target, "config", "user.email", "tests@example.invalid")
	writeScannerFile(t, target, "requirements.txt", "openai==1.2.3\n")
	writeScannerFile(t, target, "old.py", "import logging\nlogging.info(prompt)\n")
	runScannerGit(t, target, "add", ".")
	runScannerGit(t, target, "commit", "-m", "initial")
	base := strings.TrimSpace(runScannerGit(t, target, "rev-parse", "HEAD"))

	syntheticCredential := "sk-proj-" + strings.Repeat("a", 24)
	writeScannerFile(t, target, "new.py", `credential = "`+syntheticCredential+`"`+"\n")
	result, err := New().Scan(context.Background(), target, Options{ChangedSince: base})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, finding := range result.Findings {
		seen[finding.RuleID] = true
		if finding.RuleID == "AI-LOG-001" {
			t.Fatalf("unchanged code finding was reported: %#v", finding)
		}
	}
	for _, want := range []string{"AI-SEC-001", "AI-DOC-001", "AI-RISK-001"} {
		if !seen[want] {
			t.Errorf("repository-wide changed scan missing %s: %#v", want, result.Findings)
		}
	}
	if len(result.Repository.Files) != 1 || result.Repository.Files[0].Path != "new.py" {
		t.Fatalf("scoped repository = %#v", result.Repository.Files)
	}
	if len(result.FullRepository.Files) != 3 {
		t.Fatalf("full repository lost governance evidence: %#v", result.FullRepository.Files)
	}
}

func writeScannerFile(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runScannerGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return string(output)
}
