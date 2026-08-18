package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/report"
)

func TestAIUseReportLoaderPreservesIncompleteCoverageButSetupRequiresCompletion(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target, profile.NewDraftSystem("demo", "Demo"))
	reportPath := filepath.Join(target, "incomplete-report.json")
	value := report.New(target, "test", nil, nil, 0)
	value.RepositoryAnalysisRun = report.RepositoryAnalysisIncomplete
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI,
		Model:    "test",
		Coverage: providers.RepositoryCoverage{
			Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: 10, RepositoryBytes: 10_000,
			FilesSubmitted: 6, BytesSubmitted: 6_000, Subsystems: 1,
		},
		Result: providers.RepositorySectionResult{
			Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{},
			UnresolvedQuestions: []string{"Repository review completed 1 of 2 bounded source batches."},
		},
		Notes: []string{"Partial coverage is retained for diagnostics only."},
	}
	writeRawAIUseReport(t, reportPath, value)

	loaded, err := loadAIUseReport(reportPath)
	if err != nil {
		t.Fatalf("loadAIUseReport rejected truthful incomplete coverage payload: %v", err)
	}
	if loaded.RepositoryAnalysisRun != report.RepositoryAnalysisIncomplete || loaded.RepositoryAnalysis == nil || loaded.RepositoryAnalysis.Coverage.FilesSubmitted != 6 {
		t.Fatalf("incomplete repository analysis was not preserved: %#v", loaded.RepositoryAnalysis)
	}

	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{"ai-uses", "setup", "--interactive", "--report", reportPath, target}, strings.NewReader(""), &stdout, &stderr, testBuild)
	if code == 0 {
		t.Fatal("ai-uses setup accepted an incomplete repository review")
	}
	if !strings.Contains(stderr.String(), "has no completed repository AI review") {
		t.Fatalf("ai-uses setup error = %q, want intended no-completed-review guidance", stderr.String())
	}
}

func TestBatchedRepositoryCompletionWordingIncludesSourceBatchesAndSynthesis(t *testing.T) {
	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, `completionDetail := fmt.Sprintf("%d code-excerpt transfer(s)"`)
	end := strings.Index(text, `Repository AI reasoning completed in %s using %s context (%s).\n`)
	if start < 0 || end <= start {
		t.Fatal("could not isolate repository-analysis completion wording")
	}
	text = text[start:end]
	batchBranch := strings.Index(text, "repositoryReview.Coverage.Subsystems > 0")
	batchWording := strings.Index(text, "%d bounded source batch(es) plus synthesis")
	singleCallBranch := strings.Index(text, "else if repositoryReview.Coverage.Mode == providers.RepositoryAnalysisTargeted")
	if batchBranch < 0 || batchWording < 0 {
		t.Fatal("CLI completion no longer reports bounded source batches plus synthesis")
	}
	if singleCallBranch >= 0 && (batchBranch > singleCallBranch || batchWording > singleCallBranch) {
		t.Fatal("single targeted-call wording can run before the batched source-plus-synthesis wording")
	}
}

func TestTargetedRepositoryCompletionDoesNotClaimAnExactModelCallCount(t *testing.T) {
	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, `completionDetail := fmt.Sprintf("%d code-excerpt transfer(s)"`)
	endMarker := `Repository AI reasoning completed in %s using %s context (%s).\n`
	end := strings.Index(text, endMarker)
	if start < 0 || end <= start {
		t.Fatal("could not isolate repository-analysis completion wording")
	}
	block := text[start:end]
	for _, fixedCallClaim := range []string{"calls := 1", "calls = 2", "%d model call(s)", "1 model call", "2 model calls"} {
		if strings.Contains(block, fixedCallClaim) {
			t.Fatalf("CLI completion claims a fixed provider call count even though adaptive and rate-limit retries can add attempts (%q):\n%s", fixedCallClaim, block)
		}
	}
}

func TestTargetedRepositoryCompletionDoesNotClaimModelCallsForZeroSubmission(t *testing.T) {
	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	zero := strings.Index(text, "repositoryReview.Coverage.FilesSubmitted == 0")
	if zero < 0 {
		t.Fatal("zero-submission completion branch is missing")
	}
	block := text[zero:]
	if !strings.Contains(block, "no structural candidate; no source sent for repository AI review") {
		t.Fatalf("zero-submission completion lacks truthful no-call wording:\n%s", block)
	}
	if strings.Contains(block, "one or more model calls") {
		t.Fatalf("zero-submission completion contains an unsupported model-call claim:\n%s", block)
	}
}

func TestTargetedBatchProgressWordingSeparatesStartFromAnalyzed(t *testing.T) {
	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	startCase := strings.Index(text, `case "targeted-batch-start":`)
	completedCase := strings.Index(text, `case "targeted-batch":`)
	if startCase < 0 || completedCase <= startCase {
		t.Fatal("CLI no longer has ordered targeted-batch start and completion progress cases")
	}
	startBlock := text[startCase:completedCase]
	if !strings.Contains(startBlock, "Starting AI evidence batch") || strings.Contains(startBlock, " analyzed:") {
		t.Fatalf("targeted batch start can be printed as completed analysis:\n%s", startBlock)
	}
	nextCaseOffset := strings.Index(text[completedCase+1:], `case "`)
	if nextCaseOffset < 0 {
		t.Fatal("could not isolate targeted batch completion progress case")
	}
	completedBlock := text[completedCase : completedCase+1+nextCaseOffset]
	if !strings.Contains(completedBlock, "AI evidence batch") || !strings.Contains(completedBlock, " analyzed:") {
		t.Fatalf("successful targeted batch no longer receives explicit analyzed wording:\n%s", completedBlock)
	}
}

func TestProviderLimitRetryProgressExplainsThatEvidenceIsPreserved(t *testing.T) {
	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, `case "adaptive-limit-retry":`)
	if start < 0 {
		t.Fatal("CLI has no provider-limit output-reduction progress case")
	}
	next := strings.Index(text[start+1:], `case "`)
	if next < 0 {
		t.Fatal("could not isolate provider-limit retry progress case")
	}
	block := text[start : start+1+next]
	if !strings.Contains(block, "smaller response") || !strings.Contains(block, "without dropping evidence") {
		t.Fatalf("provider-limit retry does not explain the bounded retry:\n%s", block)
	}
}

func TestRepositoryValidationRepairProgressShowsSafeLocalReason(t *testing.T) {
	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, `case "validation-repair":`)
	if start < 0 {
		t.Fatal("CLI has no repository validation-repair progress case")
	}
	next := strings.Index(text[start+1:], `case "`)
	if next < 0 {
		t.Fatal("could not isolate repository validation-repair progress case")
	}
	block := text[start : start+1+next]
	if !strings.Contains(block, "Reason: %s") || !strings.Contains(block, "progress.Detail") {
		t.Fatalf("repository validation repair hides its safe local diagnostic:\n%s", block)
	}
}
