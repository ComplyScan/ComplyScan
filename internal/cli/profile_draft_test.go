package cli

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestQuickProfileUsesEditableDraftDefaults(t *testing.T) {
	draft := newSetupProfileDraft()
	draft.Suggestions["intended-purpose"] = providers.ProfileSuggestion{
		Field: "intended-purpose", Values: []string{"Draft support replies for agents."}, Confidence: "high",
		Rationale: "README purpose", Evidence: []providers.ProfileEvidence{{Path: "README.md", Line: 2, Summary: "Describes reply drafting."}},
	}
	draft.Suggestions["decision-impact"] = providers.ProfileSuggestion{Field: "decision-impact", Values: []string{"advisory"}, Confidence: "medium", Rationale: "Review workflow", Evidence: []providers.ProfileEvidence{{Path: "review.go", Summary: "Agent reviews drafts."}}}
	draft.Suggestions["lifecycle-stage"] = providers.ProfileSuggestion{Field: "lifecycle-stage", Values: []string{"testing"}, Confidence: "low", Rationale: "Test deployment", Evidence: []providers.ProfileEvidence{{Path: "deploy.yml", Summary: "Test deployment."}}}
	draft.Suggestions["human-oversight"] = providers.ProfileSuggestion{Field: "human-oversight", Values: []string{"required"}, Confidence: "medium", Rationale: "Approval gate", Evidence: []providers.ProfileEvidence{{Path: "review.go", Summary: "Approval is required."}}}
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("\n\n7\n4\n\n\n\n")), output: &output}
	system, err := collectBasicSystemProfile(prompt, t.TempDir(), time.Now(), setupRepositorySummary{}, draft)
	if err != nil {
		t.Fatal(err)
	}
	if system.IntendedPurpose != "Draft support replies for agents." || system.DecisionImpact != profile.ImpactAdvisory || system.LifecycleStage != profile.LifecycleTesting || system.HumanOversight != profile.OversightRequired {
		t.Fatalf("system = %#v", system)
	}
	if !strings.Contains(output.String(), "editable draft") || !strings.Contains(output.String(), "README.md:2") {
		t.Fatalf("draft explanation missing:\n%s", output.String())
	}
}

func TestTechnicalContextConfirmsDraftedActivitiesAndDeployment(t *testing.T) {
	draft := newSetupProfileDraft()
	draft.Suggestions["ai-activities"] = providers.ProfileSuggestion{Field: "ai-activities", Values: []string{"inference", "agent-tool-use"}, Confidence: "high", Rationale: "Agent runtime", Evidence: []providers.ProfileEvidence{{Path: "agent.go", Summary: "Calls a model and tools."}}}
	draft.Suggestions["deployment-models"] = providers.ProfileSuggestion{Field: "deployment-models", Values: []string{"api"}, Confidence: "medium", Rationale: "HTTP routes", Evidence: []providers.ProfileEvidence{{Path: "routes.go", Summary: "Exposes an API."}}}
	system := profile.NewDraftSystem("assistant", "Assistant")
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("\n\n")), output: &output}
	if err := collectTechnicalSystemContext(prompt, &system, draft, false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(stringAIActivities(system.AIActivities), ",") != "inference,agent-tool-use" || strings.Join(stringDeploymentModels(system.DeploymentModels), ",") != "api" {
		t.Fatalf("system = %#v", system)
	}
}

func TestTechnicalContextUsesInferenceAsEditableStartingPoint(t *testing.T) {
	system := profile.NewDraftSystem("assistant", "Assistant")
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("\n\n")), output: &output}
	if err := collectTechnicalSystemContext(prompt, &system, newSetupProfileDraft(), false); err != nil {
		t.Fatal(err)
	}
	if len(system.AIActivities) != 1 || system.AIActivities[0] != profile.ActivityInference {
		t.Fatalf("AI activity default = %#v", system.AIActivities)
	}
	if len(system.DeploymentModels) != 1 || system.DeploymentModels[0] != profile.DeploymentUnknown {
		t.Fatalf("deployment default = %#v", system.DeploymentModels)
	}
}
