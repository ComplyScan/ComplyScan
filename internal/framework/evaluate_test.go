package framework

import (
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func TestEvaluateMapsCodeEvidenceWithoutControlOrComplianceClaims(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	repository := discovery.Repository{Files: []discovery.File{
		{
			Path: "internal/review/override.go", Kind: discovery.KindSource,
			Content: []byte("package review\nfunc OverrideDecision(output string) error { return nil }\n"),
		},
		{
			Path: "internal/evaluation/metrics_test.go", Kind: discovery.KindSource,
			Content: []byte("package evaluation\nfunc TestMetric() { modelAccuracyThreshold := 0.95; assert(modelAccuracyThreshold > 0.9) }\n"),
		},
	}}
	report := Evaluate(pack, []profile.System{profile.NewDraftSystem("example", "Example")}, repository)
	if len(report.Systems) != 1 || report.Systems[0].ID != "example" {
		t.Fatalf("unexpected system references: %#v", report.Systems)
	}
	if report.Summary.Total != len(pack.Objectives) || report.Summary.CandidateEvidence != 2 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
	for _, objective := range report.Objectives {
		if err := objective.Applicability.Validate(); err != nil {
			t.Fatalf("evaluated objective lost applicability conditions: %s: %v", objective.ID, err)
		}
		if strings.Contains(string(objective.Status), "compliant") || strings.Contains(string(objective.Status), "satisfied") {
			t.Fatalf("objective made an unsupported conclusion: %#v", objective)
		}
		for _, match := range objective.Matches {
			if match.Path == "" || match.StartLine <= 0 || len(match.Fingerprint) != 64 || len(match.MatchedTerms) == 0 {
				t.Fatalf("untraceable evidence match: %#v", match)
			}
		}
	}
}

func TestEvaluateWorksWithoutSystemProfile(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	report := Evaluate(pack, nil, discovery.Repository{})
	if len(report.Systems) != 0 || report.Summary.NotDetected != len(pack.Objectives) {
		t.Fatalf("unexpected profile-free evidence report: %#v", report)
	}
}

func TestSharedControlProducesSameEvidenceFingerprintAcrossFrameworks(t *testing.T) {
	euPack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	nistPack, err := LoadBuiltin(NISTAIRMFTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "review/override.go", Kind: discovery.KindSource,
		Content: []byte("package review\nfunc OverrideDecision(output string) {}\n"),
	}}}
	eu := Evaluate(euPack, nil, repository)
	nist := Evaluate(nistPack, nil, repository)
	euMatch := objectiveByControl(t, eu.Objectives, "human-override").Matches
	nistMatch := objectiveByControl(t, nist.Objectives, "human-override").Matches
	if len(euMatch) != 1 || len(nistMatch) != 1 || euMatch[0].Fingerprint != nistMatch[0].Fingerprint {
		t.Fatalf("shared evidence was not stable across framework mappings: eu=%#v nist=%#v", euMatch, nistMatch)
	}
}

func objectiveByControl(t *testing.T, objectives []ObjectiveAssessment, controlID string) ObjectiveAssessment {
	t.Helper()
	for _, objective := range objectives {
		if objective.ControlID == controlID {
			return objective
		}
	}
	t.Fatalf("control %q not found", controlID)
	return ObjectiveAssessment{}
}

func TestObjectivePathSignalIsRequiredWhenConfigured(t *testing.T) {
	objective := TechnicalObjective{
		PathKeywords:  []string{"override"},
		KeywordGroups: [][]string{{"override"}, {"decision"}},
	}
	if matched, _, _ := matchesObjective("service.go", "func override decision", objective); matched {
		t.Fatal("generic path matched an objective with a configured path signal")
	}
	if matched, _, _ := matchesObjective("override/service.go", "func override decision", objective); !matched {
		t.Fatal("configured path and content signals did not match")
	}
}

func TestObjectiveTermsMustOccurWithinBoundedContext(t *testing.T) {
	objective := TechnicalObjective{
		PathKeywords:  []string{"override"},
		KeywordGroups: [][]string{{"override"}, {"decision"}},
	}
	content := "override\n" + strings.Repeat("unrelated\n", maxEvidenceLineSpan+1) + "decision\n"
	if matched, _, _ := matchesObjective("override/service.go", content, objective); matched {
		t.Fatal("unrelated terms from distant parts of a file were combined")
	}
	content += "override decision\n"
	if matched, terms, _ := matchesObjective("override/service.go", content, objective); !matched || len(terms) != 3 {
		t.Fatalf("nearby evidence was not preferred: matched=%t terms=%#v", matched, terms)
	}
}

func TestCandidateScopeRejectsDeveloperAgentMetadata(t *testing.T) {
	if candidatePassesStaticScope(
		"human-review-gate",
		".agents/skills/maintainer-review/agents/openai.yaml",
		"request approval before a decision",
		1,
	) {
		t.Fatal("developer-agent skill metadata was treated as application evidence")
	}
}

func TestCandidateScopeRejectsTracingOnlySafeStop(t *testing.T) {
	if candidatePassesStaticScope(
		"safe-stop",
		"tests/tracing/test_tracing_env_disable.py",
		"disable tracing and do not log model data",
		1,
	) {
		t.Fatal("disabling tracing was treated as stopping an AI operation")
	}
	if !candidatePassesStaticScope(
		"safe-stop",
		"internal/inference/model_shutdown.go",
		"disable model inference",
		1,
	) {
		t.Fatal("an inference shutdown path was rejected")
	}
}

func TestCandidateScopeRejectsDatasetNameSelection(t *testing.T) {
	content := "# Validate dataset names if specified\ninvalid_names = [name for name in dataset_names]\n"
	if candidatePassesStaticScope("dataset-validation", "dataset_provider.py", content, 1) {
		t.Fatal("dataset-name selection was treated as dataset quality validation")
	}
	content = "# Validate dataset schema\nmissing_fields = required - dataset.keys()\n"
	if !candidatePassesStaticScope("dataset-validation", "dataset_validation.py", content, 1) {
		t.Fatal("dataset schema validation was rejected")
	}
}

func TestCandidateScopeRejectsDatasetLoaderAsBiasEvaluation(t *testing.T) {
	if candidatePassesStaticScope(
		"bias-evaluation",
		"tests/unit/datasets/test_social_bias_dataset.py",
		"test discrimination dataset loading",
		1,
	) {
		t.Fatal("a dataset loader test was treated as bias evaluation")
	}
	if !candidatePassesStaticScope(
		"bias-evaluation",
		"tests/benchmark/test_fairness_dataset.py",
		"evaluate fairness on the dataset",
		1,
	) {
		t.Fatal("a fairness benchmark was rejected")
	}
}

func TestCandidateScopeRejectsLongEmbeddedParserFixture(t *testing.T) {
	content := "def test_markdown():\n    document = \"\"\"\n" +
		strings.Repeat("reference material\n", 120) +
		"safety evaluation recall threshold\n\"\"\"\n"
	if candidatePassesStaticScope(
		"performance-thresholds",
		"tests/node_parser/test_markdown_element.py",
		content,
		123,
	) {
		t.Fatal("an embedded document parser fixture was treated as a performance control")
	}
	if !candidatePassesStaticScope(
		"performance-thresholds",
		"tests/evaluation/test_threshold.py",
		"assert recall >= threshold\n",
		1,
	) {
		t.Fatal("an executable threshold test was rejected")
	}
}

func TestCandidateScopeRequiresPerformanceThresholdEnforcement(t *testing.T) {
	metadata := `entry = {
    "scorer_specific_params": {"threshold": 0.5},
    "metrics": {"f1_score": 0.85, "recall": 0.82},
}`
	if candidatePassesStaticScope(
		"performance-thresholds",
		"tests/unit/score/test_scorer_metrics.py",
		metadata,
		2,
	) {
		t.Fatal("threshold metadata was treated as an enforced performance threshold")
	}
	assertion := `it("fails below recall threshold", () => {
  expect(checkRecall({ score: 0.4, threshold: 0.5 }).pass).toBe(false)
})`
	if !candidatePassesStaticScope(
		"performance-thresholds",
		"test/assertions/contextRecall.test.ts",
		assertion,
		2,
	) {
		t.Fatal("an executable performance threshold assertion was rejected")
	}
}

func TestObjectivePathMatchingPreservesCamelCaseBoundaries(t *testing.T) {
	objective := TechnicalObjective{
		PathKeywords:  []string{"injection"},
		KeywordGroups: [][]string{{"prompt injection"}, {"test"}},
	}
	if matched, terms, _ := matchesObjective(
		"redteam/indirectPromptInjection.ts",
		"const id = 'prompt injection'\nconst test = true\n",
		objective,
	); !matched || len(terms) != 3 {
		t.Fatalf("camel-case path signal was not preserved: matched=%t terms=%#v", matched, terms)
	}
}

func TestDatasetObjectiveMatchesPluralSchemaPath(t *testing.T) {
	objective := TechnicalObjective{
		PathKeywords:  []string{"dataset", "schema", "schemas"},
		KeywordGroups: [][]string{{"dataset"}, {"validates dataset"}},
	}
	if matched, _, _ := matchesObjective(
		"serverApiSchemas.test.ts",
		"it validates dataset generation request bodies",
		objective,
	); !matched {
		t.Fatal("plural camel-case schema path did not match a strong dataset-validation phrase")
	}
}

func TestEvidenceMatchesAreBoundedAndDeterministic(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]discovery.File, 0, 10)
	for index := 9; index >= 0; index-- {
		files = append(files, discovery.File{
			Path: "src/risk_" + string(rune('a'+index)) + "_test.go", Kind: discovery.KindSource,
			Content: []byte("package risk\nfunc TestAISafetyValidation() {}\n"),
		})
	}
	report := Evaluate(pack, nil, discovery.Repository{Files: files})
	matches := report.Objectives[0].Matches
	if len(matches) != maxEvidenceMatches {
		t.Fatalf("matches = %d", len(matches))
	}
	for index := 1; index < len(matches); index++ {
		if matches[index-1].Path > matches[index].Path {
			t.Fatalf("matches are not sorted: %#v", matches)
		}
	}
}

func TestEvidenceDefinitionFilesCanOptOutOfSelfMatching(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "technical-pack.yml", Kind: discovery.KindConfig,
		Content: []byte("# complyscan:ignore-technical-evidence\noverride decision\n"),
	}}}
	report := Evaluate(pack, nil, repository)
	if report.Summary.CandidateEvidence != 0 {
		t.Fatalf("definition file matched itself: %#v", report.Objectives)
	}
}

func TestEvaluateAttachesBoundedProductionContext(t *testing.T) {
	pack := Pack{Objectives: []TechnicalObjective{{
		ID: "eu-aia-14-override-intervention", Title: "Override", SourceReference: "Article 14",
		Description: "Override decision", FileKinds: []string{"source"},
		PathKeywords: []string{"override"}, KeywordGroups: [][]string{{"override"}, {"decision"}},
	}}}
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "override/main.go", Kind: discovery.KindSource,
		Content: []byte(`package main
func main() { overrideDecision() }
func overrideDecision() { authorizeReviewer() }
func authorizeReviewer() {}
`),
	}}}
	report := Evaluate(pack, nil, repository)
	if report.SchemaVersion != 2 || report.Analysis.FilesIndexed != 1 || report.Analysis.RelationshipsIndexed < 2 {
		t.Fatalf("unexpected graph coverage: %#v", report.Analysis)
	}
	match := report.Objectives[0].Matches[0]
	if match.Context.Anchor == nil || match.Context.Anchor.QualifiedName != "main.overrideDecision" || match.Context.Anchor.Reachability != "production-reachable" {
		t.Fatalf("unexpected context anchor: %#v", match.Context)
	}
	for _, question := range match.Context.UnresolvedQuestions {
		if strings.Contains(question, "authorization") {
			t.Fatalf("resolved authorization was reported unresolved: %#v", match.Context)
		}
	}
}

func TestEvaluateRequiresKeywordBoundaries(t *testing.T) {
	pack := Pack{Objectives: []TechnicalObjective{{
		ID: "metric-threshold", Title: "Metric threshold", SourceReference: "test", Description: "test",
		FileKinds: []string{"source"}, PathKeywords: []string{"test"},
		KeywordGroups: [][]string{{"f1"}, {"assert"}}, Verification: "technical-and-human",
	}}}
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "metric_test.go", Kind: discovery.KindSource,
		Content: []byte("package metric\nconst f16 = 1\nfunc assertive() {}\n"),
	}}}
	report := Evaluate(pack, nil, repository)
	if report.Objectives[0].Status != ObjectiveNotDetected {
		t.Fatalf("embedded substrings created evidence: %#v", report.Objectives[0])
	}
	repository.Files[0].Content = []byte("package metric\nfunc TestMetric() {\n f1 := 1\n assert(f1 > 0)\n}\n")
	report = Evaluate(pack, nil, repository)
	if report.Objectives[0].Status != ObjectiveCandidate || len(report.Objectives[0].Matches) != 1 {
		t.Fatalf("bounded keywords were not detected: %#v", report.Objectives[0])
	}
}

func TestEvaluateMarksUnsupportedSourceObjectivesNotEvaluated(t *testing.T) {
	pack := Pack{Objectives: []TechnicalObjective{{
		ID: "source-control", FileKinds: []string{"source"},
		PathKeywords: []string{"worker"}, KeywordGroups: [][]string{{"decision"}},
	}}}
	report := Evaluate(pack, nil, discovery.Repository{Files: []discovery.File{
		{Path: "main.go", Kind: discovery.KindSource, Content: []byte("package main\nfunc main() {}\n")},
		{Path: "worker.rb", Kind: discovery.KindSource, Content: []byte("decision = model.predict(value)")},
	}})
	if report.Objectives[0].Status != ObjectiveNotEvaluated || report.Summary.NotEvaluated != 1 {
		t.Fatalf("unsupported source was treated as evaluated: %#v", report)
	}
}
