// complyscan:ignore-technical-evidence -- this file asserts synthetic objective fixtures.
package framework

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

func TestTechnicalContextFixtureDistinguishesLiveAndTestOnlyOverrideEvidence(t *testing.T) {
	discovered, err := discovery.Discover(context.Background(), filepath.Join("..", "..", "testdata", "technical-context-go"), discovery.Options{})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	report := Evaluate(pack, nil, discovered.Repository)
	var live, dead *EvidenceMatch
	for objectiveIndex := range report.Objectives {
		objective := &report.Objectives[objectiveIndex]
		if objective.ID != "eu-aia-14-override-intervention" {
			continue
		}
		for matchIndex := range objective.Matches {
			match := &objective.Matches[matchIndex]
			if match.Context.Anchor == nil {
				continue
			}
			switch match.Context.Anchor.QualifiedName {
			case "main.handleOverrideDecision":
				live = match
			case "main.deadOverrideDecision":
				dead = match
			}
		}
	}
	if live == nil || dead == nil {
		t.Fatalf("fixture candidates missing: live=%#v dead=%#v", live, dead)
	}
	if live.Context.Anchor.Reachability != "production-reachable" || dead.Context.Anchor.Reachability != "test-only" {
		t.Fatalf("unexpected fixture reachability: live=%#v dead=%#v", live.Context.Anchor, dead.Context.Anchor)
	}
	if containsQuestion(live.Context.UnresolvedQuestions, "authorization") {
		t.Fatalf("live authorization was reported unresolved: %#v", live.Context)
	}
	if !containsQuestion(dead.Context.UnresolvedQuestions, "authorization") || !containsQuestion(dead.Context.UnresolvedQuestions, "only from indexed tests") {
		t.Fatalf("test-only missing-auth candidate was not challenged: %#v", dead.Context)
	}
}

func TestPythonTechnicalContextFixtureDistinguishesLiveAndTestOnlyOverrideEvidence(t *testing.T) {
	discovered, err := discovery.Discover(context.Background(), filepath.Join("..", "..", "testdata", "technical-context-python"), discovery.Options{})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	report := Evaluate(pack, nil, discovered.Repository)
	var live, dead *EvidenceMatch
	for objectiveIndex := range report.Objectives {
		objective := &report.Objectives[objectiveIndex]
		if objective.ID != "eu-aia-14-override-intervention" {
			continue
		}
		for matchIndex := range objective.Matches {
			match := &objective.Matches[matchIndex]
			if match.Context.Anchor == nil {
				continue
			}
			switch match.Context.Anchor.QualifiedName {
			case "override.main.handle_override_decision":
				live = match
			case "override.dead_override.dead_override_decision":
				dead = match
			}
		}
	}
	if live == nil || dead == nil {
		t.Fatalf("Python fixture candidates missing: live=%#v dead=%#v", live, dead)
	}
	if live.Context.Anchor.Reachability != "production-reachable" || dead.Context.Anchor.Reachability != "test-only" {
		t.Fatalf("unexpected Python fixture reachability: live=%#v dead=%#v", live.Context.Anchor, dead.Context.Anchor)
	}
	if containsQuestion(live.Context.UnresolvedQuestions, "authorization") {
		t.Fatalf("live Python authorization was reported unresolved: %#v", live.Context)
	}
	if !containsQuestion(dead.Context.UnresolvedQuestions, "authorization") || !containsQuestion(dead.Context.UnresolvedQuestions, "only from indexed tests") {
		t.Fatalf("test-only Python candidate was not challenged: %#v", dead.Context)
	}
}

func containsQuestion(questions []string, fragment string) bool {
	for _, question := range questions {
		if strings.Contains(strings.ToLower(question), strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}
