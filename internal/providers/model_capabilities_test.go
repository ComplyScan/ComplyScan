package providers

import "testing"

func TestRepositoryModelCapabilitiesKeepsContextSeparateFromRateLimits(t *testing.T) {
	for _, model := range []string{"gpt-5.6", "gpt-5.6-sol", "gpt-5.6-sol-2026-08-01"} {
		got := RepositoryModelCapabilities(OpenAI, model)
		if got.ContextWindowTokens != 1_050_000 || got.MaxOutputTokens != OpenAIMaxOutputTokens {
			t.Fatalf("OpenAI model %q capabilities = %#v", model, got)
		}
	}
	if got := RepositoryModelCapabilities(OpenAI, "unknown-model"); got != (ModelCapabilities{}) {
		t.Fatalf("unknown model capabilities = %#v, want conservative unknown", got)
	}
	if got := RepositoryModelCapabilities(Compatible, "gpt-5.6-sol"); got != (ModelCapabilities{}) {
		t.Fatalf("compatible gateway inherited unverified OpenAI limits: %#v", got)
	}
}
