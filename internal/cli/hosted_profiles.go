package cli

import "strings"

const customCompatibleProvider = "openai-compatible"

type hostedProviderProfile struct {
	ID        string
	Label     string
	BaseURL   string
	APIKeyEnv string
	Models    []string
}

func hostedProviderProfiles() []hostedProviderProfile {
	return []hostedProviderProfile{
		{ID: "openai", Label: "OpenAI", APIKeyEnv: "OPENAI_API_KEY", Models: []string{"gpt-5.6-terra", "gpt-5.6-sol", "gpt-5.6-luna"}},
		{ID: "anthropic", Label: "Anthropic", APIKeyEnv: "ANTHROPIC_API_KEY", Models: []string{"claude-sonnet-5", "claude-opus-5", "claude-haiku-4-5"}},
		{ID: "gemini", Label: "Google Gemini", APIKeyEnv: "GEMINI_API_KEY", Models: []string{"gemini-3.6-flash", "gemini-3.5-flash-lite", "gemini-3.5-flash"}},
		{ID: "xai", Label: "xAI — Grok models", BaseURL: "https://api.x.ai/v1", APIKeyEnv: "XAI_API_KEY", Models: []string{"grok-4.5"}},
		{ID: "mistral", Label: "Mistral", BaseURL: "https://api.mistral.ai/v1", APIKeyEnv: "MISTRAL_API_KEY", Models: []string{"mistral-large-latest", "devstral-2", "mistral-small-latest"}},
		{ID: "groq", Label: "GroqCloud — fast hosted models from several model makers", BaseURL: "https://api.groq.com/openai/v1", APIKeyEnv: "GROQ_API_KEY", Models: []string{"openai/gpt-oss-120b"}},
		{ID: "openrouter", Label: "OpenRouter — many providers and models", BaseURL: "https://openrouter.ai/api/v1", APIKeyEnv: "OPENROUTER_API_KEY", Models: []string{"openai/gpt-5.6-terra"}},
		{ID: customCompatibleProvider, Label: "Other OpenAI-compatible API"},
	}
}

func hostedProviderProfileFor(id string) (hostedProviderProfile, bool) {
	for _, profile := range hostedProviderProfiles() {
		if strings.EqualFold(profile.ID, strings.TrimSpace(id)) {
			return profile, true
		}
	}
	return hostedProviderProfile{}, false
}

func isOpenAICompatibleProvider(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "xai", "groq", "mistral", "openrouter", customCompatibleProvider:
		return true
	default:
		return false
	}
}
