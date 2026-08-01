package rules

import (
	"regexp"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

type aiPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

type aiMatch struct {
	Name     string
	Path     string
	Line     int
	Evidence string
}

var aiPatterns = []aiPattern{
	{Name: "OpenAI", Pattern: regexp.MustCompile(`(?i)(\bopenai\b|api\.openai\.com|OPENAI_API_KEY)`)},
	{Name: "Anthropic", Pattern: regexp.MustCompile(`(?i)(\banthropic\b|api\.anthropic\.com|ANTHROPIC_API_KEY)`)},
	{Name: "Google Gemini", Pattern: regexp.MustCompile(`(?i)(\bgemini\b|google\.generativeai|google-genai|@google/(?:generative-ai|genai)|google\.golang\.org/genai|generativelanguage\.googleapis\.com|GEMINI_API_KEY)`)},
	{Name: "Mistral", Pattern: regexp.MustCompile(`(?i)(\bmistral(?:ai)?\b|api\.mistral\.ai|MISTRAL_API_KEY)`)},
	{Name: "Cohere", Pattern: regexp.MustCompile(`(?i)(\bcohere\b|api\.cohere\.ai|COHERE_API_KEY)`)},
	{Name: "Hugging Face", Pattern: regexp.MustCompile(`(?i)(hugging[ _-]?face|huggingface_hub|from\s+transformers\b|api-inference\.huggingface\.co|HF_TOKEN)`)},
	{Name: "Ollama", Pattern: regexp.MustCompile(`(?i)(\bollama\b|localhost:11434|OLLAMA_HOST)`)},
	{Name: "LiteLLM", Pattern: regexp.MustCompile(`(?i)(\blitellm\b)`)},
	{Name: "LangChain", Pattern: regexp.MustCompile(`(?i)(\blangchain(?:[._/-][a-z0-9_-]+)?\b)`)},
	{Name: "LlamaIndex", Pattern: regexp.MustCompile(`(?i)(\bllama[_-]?index\b)`)},
	{Name: "Vercel AI SDK", Pattern: regexp.MustCompile(`(?i)(@ai-sdk/[a-z0-9_-]+|@vercel/ai|from\s+["']ai["']|["']ai["']\s*:\s*["'])`)},
	{Name: "OpenRouter", Pattern: regexp.MustCompile(`(?i)(\bopenrouter\b|openrouter\.ai/api|OPENROUTER_API_KEY)`)},
}

func detectAIUsage(repo discovery.Repository) []aiMatch {
	var matches []aiMatch
	_ = visitAIUsage(repo, func(match aiMatch) error {
		matches = append(matches, match)
		return nil
	})
	return matches
}

func visitAIUsage(repo discovery.Repository, visit func(aiMatch) error) error {
	seen := make(map[string]struct{})
	for _, file := range repo.Files {
		if !isTechnical(file.Kind) {
			continue
		}
		for lineIndex, line := range lines(file.Content) {
			for _, candidate := range aiPatterns {
				if !candidate.Pattern.MatchString(line) {
					continue
				}
				key := candidate.Name + "\x00" + file.Path
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				if err := visit(aiMatch{
					Name: candidate.Name, Path: file.Path, Line: lineIndex + 1,
					Evidence: sanitizeEvidence(line, 160),
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func isTechnical(kind discovery.FileKind) bool {
	switch kind {
	case discovery.KindReadme, discovery.KindDocumentation, discovery.KindModelCard,
		discovery.KindPrivacy, discovery.KindRisk, discovery.KindAIGovernance:
		return false
	default:
		return true
	}
}

func lines(content []byte) []string {
	return strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
}

func sanitizeEvidence(value string, limit int) string {
	value = strings.TrimSpace(RedactSecrets(value))
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return value
}
