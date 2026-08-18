package providers

import "strings"

// ModelCapabilities contains stable per-request limits published for a model.
// Rolling RPM/TPM capacity is intentionally not represented here; adapters
// learn that separate account-level state from live response headers.
type ModelCapabilities struct {
	ContextWindowTokens int
	MaxOutputTokens     int
}

// RepositoryModelCapabilities returns only limits ComplyScan can identify
// confidently from the configured provider/model pair. Unknown and compatible
// models return zero values and retain the conservative adaptive fallback.
func RepositoryModelCapabilities(provider Kind, model string) ModelCapabilities {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch provider {
	case OpenAI:
		// The GPT-5.6 family shares the published 1.05M context and 128K
		// output limits. Snapshot suffixes retain the same family prefix.
		if normalized == "gpt-5.6" || strings.HasPrefix(normalized, "gpt-5.6-") {
			return ModelCapabilities{ContextWindowTokens: 1_050_000, MaxOutputTokens: OpenAIMaxOutputTokens}
		}
	case Ollama:
		// Ollama model tags do not encode a portable context ceiling. The
		// request adapter sets num_ctx explicitly from each bounded payload.
		return ModelCapabilities{}
	}
	return ModelCapabilities{}
}
