package profile

import (
	"reflect"
	"testing"
)

func TestCodeFactDeploymentValuesRemainTechnicalMechanisms(t *testing.T) {
	values, ok := CodeFactAllowedValues(CodeFactDeploymentModels)
	want := []string{"embedded", "api", "local-cli"}
	if !ok || !reflect.DeepEqual(values, want) {
		t.Fatalf("deployment fact values = %v, want %v", values, want)
	}
	for _, forbidden := range []string{"internal", "private-customer", "public", "open-source", "production"} {
		if CodeFactAllowsValue(CodeFactDeploymentModels, forbidden) {
			t.Fatalf("organisation-level deployment value %q was accepted as a code fact", forbidden)
		}
	}
}

func TestCodeFactContractRejectsNegativeAndLegalContext(t *testing.T) {
	for _, field := range []string{"operating-regions", "organization-roles", "applicability-decision"} {
		if _, ok := ParseCodeFactField(field); ok {
			t.Fatalf("legal or organisation field %q entered the code-fact taxonomy", field)
		}
	}
	for _, value := range []string{"", "unknown", "none", "no", "not established", "not detected"} {
		if CodeFactAllowsValue(CodeFactIntendedPurpose, value) {
			t.Fatalf("negative or placeholder value %q was accepted", value)
		}
	}
	if !CodeFactAllowsValue(CodeFactIntendedPurpose, "Drafts support replies for review") {
		t.Fatal("positive free-text purpose was rejected")
	}
	if !CodeFactPositiveOnly(CodeFactPersonalData) || CodeFactAllowsValue(CodeFactPersonalData, "no") || !CodeFactAllowsValue(CodeFactPersonalData, "yes") {
		t.Fatal("personal-data positive-only contract changed")
	}
}
