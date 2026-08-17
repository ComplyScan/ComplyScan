package reviewcontext

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
)

func TestBuildRedactsMatchedAnchorAndRelatedSourceWithoutChangingCitationMetadata(t *testing.T) {
	anchorSecret := "sk-proj-" + strings.Repeat("a", 24)
	relatedSecret := "sk-ant-api03-" + strings.Repeat("b", 24)
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "review/control.go", Kind: discovery.KindSource,
		Content: []byte(fmt.Sprintf(`package review

func main() { reviewDecision() }

func reviewDecision() bool {
	token := %q
	_ = token
	return authorizeReviewer()
}

func authorizeReviewer() bool {
	token := %q
	_ = token
	return true
}
`, anchorSecret, relatedSecret)),
	}}}
	graph := codegraph.Build(repository)
	context := graph.ContextForMatch("review/control.go", 5, []string{"review decision"}, 20)
	if context.Anchor == nil {
		t.Fatal("test repository did not produce an anchor")
	}
	evidence := framework.TechnicalEvidenceReport{Objectives: []framework.ObjectiveAssessment{{
		ID: "human-review", Title: "Human review", Matches: []framework.EvidenceMatch{{
			Fingerprint: strings.Repeat("f", 64), Path: "review/control.go", StartLine: 5, Context: context,
		}},
	}}}

	request := Build(evidence, repository)
	if len(request.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(request.Candidates))
	}
	candidate := request.Candidates[0]
	seen := map[string]bool{}
	for _, source := range candidate.SourceContexts {
		if strings.Contains(source.Source, anchorSecret) || strings.Contains(source.Source, relatedSecret) {
			t.Fatalf("%s context retained a raw credential: %q", source.Role, source.Source)
		}
		if source.Path != "review/control.go" {
			t.Fatalf("redaction changed citation path: %#v", source)
		}
		switch source.Role {
		case "matched-evidence":
			seen[source.Role] = true
			if source.StartLine != 5 || !strings.Contains(source.Source, "sk-proj-****aaaa") {
				t.Fatalf("matched evidence lost line binding or canonical redaction: %#v", source)
			}
		case "anchor":
			seen[source.Role] = true
			if source.StartLine != context.Anchor.StartLine || source.EndLine != context.Anchor.EndLine || !strings.Contains(source.Source, "sk-proj-****aaaa") {
				t.Fatalf("anchor lost symbol citation or canonical redaction: %#v", source)
			}
		case "related":
			if strings.Contains(source.Source, "authorizeReviewer") {
				seen[source.Role] = true
				if !strings.Contains(source.Source, "sk-ant-api03-****bbbb") {
					t.Fatalf("related symbol did not use canonical redaction: %#v", source)
				}
			}
		}
	}
	for _, role := range []string{"matched-evidence", "anchor", "related"} {
		if !seen[role] {
			t.Fatalf("test did not exercise %s source: %#v", role, candidate.SourceContexts)
		}
	}
}

func TestRelationshipSourceRedactionPreservesPathAndLine(t *testing.T) {
	secret := "sk-or-v1-" + strings.Repeat("r", 24)
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "routes/api.go", Kind: discovery.KindSource,
		Content: []byte(fmt.Sprintf("package routes\nfunc route() { token := %q; _ = token }\n", secret)),
	}}}
	candidate := providers.TechnicalCandidate{SourceContexts: []providers.TechnicalSourceContext{}}
	remaining := appendTechnicalRelationshipSource(&candidate, repository, codegraph.Relationship{
		Kind: codegraph.EdgeRoute, Path: "routes/api.go", Line: 2, Label: "POST /review",
	}, 2_000)
	if remaining >= 2_000 || len(candidate.SourceContexts) != 1 {
		t.Fatalf("relationship source was not added: remaining=%d contexts=%#v", remaining, candidate.SourceContexts)
	}
	source := candidate.SourceContexts[0]
	if strings.Contains(source.Source, secret) || !strings.Contains(source.Source, "sk-or-v1-****rrrr") {
		t.Fatalf("relationship source was not redacted: %#v", source)
	}
	if source.Path != "routes/api.go" || source.StartLine != 2 || source.EndLine != 2 {
		t.Fatalf("relationship citation metadata changed: %#v", source)
	}
}

func TestTechnicalSourceIsRedactedBeforeTruncationCanSplitCredential(t *testing.T) {
	secret := "sk-proj-" + strings.Repeat("x", 24)
	prefix := `token = "`
	// This bound would cut the raw credential before the minimum recognised
	// length. Redacting the full value first must prevent that prefix leaking.
	value := boundTechnicalSource(prefix+secret+`"`, len([]rune(prefix))+10)
	if strings.Contains(value, "sk-proj-xx") || !strings.Contains(value, "sk-proj-**") {
		t.Fatalf("credential was truncated before canonical redaction: %q", value)
	}
	if strings.Count(value, "\n") != 0 {
		t.Fatalf("redaction changed source line structure: %q", value)
	}
}

func TestExtendedAndModelFollowUpExcerptsAreRedactedBeforeReturn(t *testing.T) {
	extendedSecret := "AIza" + strings.Repeat("e", 24)
	followUpSecret := "hf_" + strings.Repeat("f", 24)
	repository := discovery.Repository{Files: []discovery.File{
		{
			Path: "review/approval.go", Kind: discovery.KindSource,
			Content: []byte(fmt.Sprintf("package review\nfunc approveResult() bool { token := %q; _ = token; return true }\n", extendedSecret)),
		},
		{
			Path: "review/follow_up.go", Kind: discovery.KindSource,
			Content: []byte(fmt.Sprintf("package review\nfunc inspectCaller() { token := %q; _ = token }\n", followUpSecret)),
		},
	}}
	evidence := framework.TechnicalEvidenceReport{
		Pack: framework.PackReference{Digest: strings.Repeat("d", 64)},
		Objectives: []framework.ObjectiveAssessment{{
			ID: "human-review", Status: framework.ObjectiveNotDetected,
			EligibleFileKinds: []string{"source"}, InvestigationTerms: []string{"approveResult"},
		}},
	}
	mapping := reconciliation.Report{Systems: []reconciliation.SystemResult{{
		SystemID: "review", Objectives: []reconciliation.ObjectiveResult{{
			ObjectiveID: "human-review", Requirement: reconciliation.RequirementLikelyRequired,
		}},
	}}}

	request := BuildInvestigations(evidence, repository, mapping)
	if len(request.Candidates) != 1 {
		t.Fatalf("investigation count = %d, want 1", len(request.Candidates))
	}
	extended := sourceContextWithRole(request.Candidates[0].SourceContexts, "extended-search-hit")
	if extended == nil || strings.Contains(extended.Source, extendedSecret) || !strings.Contains(extended.Source, "AIza****eeee") {
		t.Fatalf("extended search source was not redacted: %#v", extended)
	}
	if extended.Path != "review/approval.go" || extended.StartLine != 2 {
		t.Fatalf("extended search citation metadata changed: %#v", extended)
	}

	followUpCandidate := providers.TechnicalCandidate{
		EligibleFileKinds: []string{"source"}, AllowedPaths: []string{"review/follow_up.go"},
	}
	updated, added := ApplyFollowUp(followUpCandidate, providers.TechnicalSearchPlan{
		Needed: true, Queries: []providers.TechnicalSearchQuery{{Text: "inspectCaller"}},
	}, repository)
	followUp := sourceContextWithRole(updated.SourceContexts, "model-directed-follow-up")
	if added != 1 || followUp == nil || strings.Contains(followUp.Source, followUpSecret) || !strings.Contains(followUp.Source, "hf_****ffff") {
		t.Fatalf("model follow-up source was not redacted: added=%d context=%#v", added, followUp)
	}
	if followUp.Path != "review/follow_up.go" || followUp.StartLine != 2 {
		t.Fatalf("follow-up citation metadata changed: %#v", followUp)
	}
}

func sourceContextWithRole(contexts []providers.TechnicalSourceContext, role string) *providers.TechnicalSourceContext {
	for index := range contexts {
		if contexts[index].Role == role {
			return &contexts[index]
		}
	}
	return nil
}
