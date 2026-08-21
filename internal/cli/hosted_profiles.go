package cli

import "strings"

const customCompatibleProvider = "openai-compatible"

type hostedProviderProfile struct {
	ID            string
	Label         string
	SetupLabel    string
	BaseURL       string
	APIKeyEnv     string
	StandardSetup bool
	Models        []hostedModelProfile
}

type hostedModelProfile struct {
	ID             string
	Role           string
	DraftValidated bool
	CodeValidated  bool
}

func hostedProviderProfiles() []hostedProviderProfile {
	return []hostedProviderProfile{
		{ID: "openai", Label: "OpenAI", SetupLabel: "OpenAI — selected frontier models", APIKeyEnv: "OPENAI_API_KEY", StandardSetup: true, Models: []hostedModelProfile{
			{ID: "gpt-5.6-sol", Role: "quality-first"},
			{ID: "gpt-5.6-terra", Role: "balanced quality and cost"},
		}},
		{ID: "anthropic", Label: "Anthropic", SetupLabel: "Anthropic — selected Claude models", APIKeyEnv: "ANTHROPIC_API_KEY", StandardSetup: true, Models: []hostedModelProfile{
			{ID: "claude-opus-5", Role: "quality-first"},
			{ID: "claude-sonnet-5", Role: "balanced quality and cost"},
		}},
		{ID: "gemini", Label: "Google Gemini", SetupLabel: "Google Gemini — selected stable models", APIKeyEnv: "GEMINI_API_KEY", StandardSetup: true, Models: []hostedModelProfile{
			{ID: "gemini-3.7-flash", Role: "quality-first coding"},
			{ID: "gemini-3.6-flash", Role: "balanced quality and efficiency"},
		}},
		{ID: "xai", Label: "xAI — experimental integration", BaseURL: "https://api.x.ai/v1", APIKeyEnv: "XAI_API_KEY", Models: []hostedModelProfile{{ID: "grok-4.5", Role: "experimental; no maintained ComplyScan quality benchmark"}}},
		{ID: "mistral", Label: "Mistral — experimental integration", BaseURL: "https://api.mistral.ai/v1", APIKeyEnv: "MISTRAL_API_KEY", Models: []hostedModelProfile{
			{ID: "mistral-large-latest", Role: "experimental; no maintained ComplyScan quality benchmark"},
			{ID: "devstral-2", Role: "experimental; no maintained ComplyScan quality benchmark"},
			{ID: "mistral-small-latest", Role: "experimental; no maintained ComplyScan quality benchmark"},
		}},
		{ID: "groq", Label: "GroqCloud — experimental hosted catalogue", BaseURL: "https://api.groq.com/openai/v1", APIKeyEnv: "GROQ_API_KEY", Models: []hostedModelProfile{{ID: "openai/gpt-oss-120b", Role: "experimental; no maintained ComplyScan quality benchmark"}}},
		{ID: "openrouter", Label: "OpenRouter — experimental multi-provider gateway", BaseURL: "https://openrouter.ai/api/v1", APIKeyEnv: "OPENROUTER_API_KEY", Models: []hostedModelProfile{{ID: "openai/gpt-5.6-terra", Role: "experimental gateway route; no maintained ComplyScan quality benchmark"}}},
		{ID: customCompatibleProvider, Label: "Other OpenAI-compatible API"},
	}
}

func standardHostedProviderProfiles() []hostedProviderProfile {
	profiles := hostedProviderProfiles()
	standard := make([]hostedProviderProfile, 0, len(profiles))
	for _, profile := range profiles {
		if profile.StandardSetup {
			standard = append(standard, profile)
		}
	}
	return standard
}

func hostedProviderProfileFor(id string) (hostedProviderProfile, bool) {
	for _, profile := range hostedProviderProfiles() {
		if strings.EqualFold(profile.ID, strings.TrimSpace(id)) {
			return profile, true
		}
	}
	return hostedProviderProfile{}, false
}

func hostedModelProfileFor(provider, model string) (hostedModelProfile, bool) {
	profile, exists := hostedProviderProfileFor(provider)
	if !exists {
		return hostedModelProfile{}, false
	}
	for _, candidate := range profile.Models {
		if strings.EqualFold(candidate.ID, strings.TrimSpace(model)) {
			return candidate, true
		}
	}
	return hostedModelProfile{}, false
}

func hostedModelStatus(model hostedModelProfile) string {
	status := "live quality gates pending"
	switch {
	case model.DraftValidated && model.CodeValidated:
		status = "validated for experimental profile assistance and technical evidence review"
	case model.DraftValidated:
		status = "validated for experimental profile assistance; technical review benchmark pending"
	case model.CodeValidated:
		status = "validated for technical evidence review; profile-assistance benchmark pending"
	}
	return model.Role + "; " + status
}

func isOpenAICompatibleProvider(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "xai", "groq", "mistral", "openrouter", customCompatibleProvider:
		return true
	default:
		return false
	}
}
