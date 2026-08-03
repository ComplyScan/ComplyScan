package framework

import (
	"strings"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/profile"
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
			Content: []byte("package evaluation\nconst accuracyThreshold = 0.95\n"),
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

func TestEvidenceMatchesAreBoundedAndDeterministic(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]discovery.File, 0, 10)
	for index := 9; index >= 0; index-- {
		files = append(files, discovery.File{
			Path: "src/risk_" + string(rune('a'+index)) + "_test.go", Kind: discovery.KindSource,
			Content: []byte("package risk\nfunc TestSafetyValidation() {}\n"),
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
