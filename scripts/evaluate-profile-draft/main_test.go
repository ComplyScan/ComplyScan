package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/profiledraft"
)

func TestFilterCasesKeepsManifestOrder(t *testing.T) {
	cases := []profiledraft.BenchmarkCase{{ID: "first"}, {ID: "second"}, {ID: "third"}}
	filtered, err := filterCases(cases, []string{"third", "first"})
	if err != nil {
		t.Fatal(err)
	}
	want := []profiledraft.BenchmarkCase{{ID: "first"}, {ID: "third"}}
	if !reflect.DeepEqual(filtered, want) {
		t.Fatalf("filtered cases = %#v, want %#v", filtered, want)
	}
}

func TestNewProfileDraftProviderRequiresEnvironmentCredentialForCloud(t *testing.T) {
	_, err := newProfileDraftProvider("openai", "gpt-5.6-sol", "", "COMPLYSCAN_MISSING_TEST_KEY", time.Second)
	if err == nil || !strings.Contains(err.Error(), "COMPLYSCAN_MISSING_TEST_KEY is not set") {
		t.Fatalf("credential error = %v", err)
	}
	_, err = newProfileDraftProvider("unsupported", "model", "", "COMPLYSCAN_MISSING_TEST_KEY", time.Second)
	if err == nil || !strings.Contains(err.Error(), "unsupported profile-draft provider") {
		t.Fatalf("provider error = %v", err)
	}
}

func TestNewProfileDraftProviderBuildsSupportedCloudReviewers(t *testing.T) {
	t.Setenv("COMPLYSCAN_PROFILE_DRAFT_TEST_KEY", "test-secret")
	for _, providerName := range []string{"openai", "anthropic", "gemini"} {
		reviewer, err := newProfileDraftProvider(providerName, "test-model", "", "COMPLYSCAN_PROFILE_DRAFT_TEST_KEY", time.Second)
		if err != nil || reviewer == nil {
			t.Errorf("%s reviewer = %#v, error = %v", providerName, reviewer, err)
		}
	}
}

func TestFilterCasesRejectsUnknownID(t *testing.T) {
	_, err := filterCases([]profiledraft.BenchmarkCase{{ID: "known"}}, []string{"missing"})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected unknown case error, got %v", err)
	}
}
