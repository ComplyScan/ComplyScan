package rules

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

type HardcodedSecretRule struct{}

type secretPattern struct {
	Provider    string
	Pattern     *regexp.Regexp
	SecretGroup int
}

var secretPatterns = []secretPattern{
	{Provider: "Anthropic", Pattern: regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(sk-ant-(?:api[0-9]{2}-)?[A-Za-z0-9_-]{16,})`), SecretGroup: 1},
	{Provider: "OpenRouter", Pattern: regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(sk-or-v1-[A-Za-z0-9_-]{16,})`), SecretGroup: 1},
	{Provider: "OpenAI", Pattern: regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(sk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{20,})`), SecretGroup: 1},
	{Provider: "Google", Pattern: regexp.MustCompile(`(?:^|[^A-Za-z0-9])(AIza[0-9A-Za-z_-]{20,})`), SecretGroup: 1},
	{Provider: "Hugging Face", Pattern: regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(hf_[A-Za-z0-9]{20,})`), SecretGroup: 1},
	{
		Provider:    "AI provider",
		Pattern:     regexp.MustCompile(`(?i)["']?((?:OPENAI|ANTHROPIC|GEMINI|GOOGLE|MISTRAL|COHERE|HUGGINGFACE|HF|OPENROUTER)(?:_API)?_(?:KEY|TOKEN))["']?\s*[:=]\s*["']([^"'\r\n]{16,})["']`),
		SecretGroup: 2,
	},
}

var environmentReferencePattern = regexp.MustCompile(`(?i)(os\.getenv|os\.environ|process\.env|system\.getenv|env::var|viper\.getstring|getenv\s*\(|\$\{?[A-Z][A-Z0-9_]+\}?)`)

type secretMatch struct {
	Provider string
	Secret   string
}

func (HardcodedSecretRule) ID() string { return "AI-SEC-001" }

func (rule HardcodedSecretRule) Run(ctx context.Context, repo discovery.Repository) ([]Finding, error) {
	return collectFindings(func(emit FindingEmitter) error {
		return rule.RunStreaming(ctx, repo, emit)
	})
}

func (HardcodedSecretRule) RunStreaming(ctx context.Context, repo discovery.Repository, emit FindingEmitter) error {
	for _, file := range repo.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		for lineIndex, line := range lines(file.Content) {
			if environmentReferencePattern.MatchString(line) {
				continue
			}
			matches := findSecrets(line)
			for _, match := range matches {
				if err := emit(Finding{
					RuleID: "AI-SEC-001", Title: "Potential hardcoded AI API credential",
					Severity: SeverityHigh, Category: "secrets-management",
					Message:     "A value matching a possible " + match.Provider + " credential pattern appears to be hardcoded. The value is redacted in this report.",
					Path:        file.Path,
					StartLine:   lineIndex + 1,
					EndLine:     lineIndex + 1,
					Evidence:    sanitizeEvidence(line, 160),
					Remediation: "Revoke and rotate the credential if it is real, remove it from source and history, and load secrets from an approved secret store or environment variable.",
					Confidence:  "high",
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func findSecrets(value string) []secretMatch {
	seen := make(map[string]struct{})
	var matches []secretMatch
	for _, candidate := range secretPatterns {
		for _, indexes := range candidate.Pattern.FindAllStringSubmatchIndex(value, -1) {
			groupStart := candidate.SecretGroup * 2
			if len(indexes) <= groupStart+1 || indexes[groupStart] < 0 {
				continue
			}
			secret := value[indexes[groupStart]:indexes[groupStart+1]]
			if candidate.Provider == "AI provider" && isObviousCredentialPlaceholder(secret) {
				continue
			}
			if _, ok := seen[secret]; ok {
				continue
			}
			seen[secret] = struct{}{}
			matches = append(matches, secretMatch{Provider: candidate.Provider, Secret: secret})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Secret < matches[j].Secret })
	return matches
}

func isObviousCredentialPlaceholder(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, "<>[]{}()")
	for _, marker := range []string{
		"your-api-key",
		"your_api_key",
		"your-key",
		"your_key",
		"replace-me",
		"replace_me",
		"example-key",
		"example_key",
		"example-token",
		"example_token",
		"example-placeholder",
		"dummy-key",
		"dummy_key",
		"fake-key",
		"fake_key",
		"sample-key",
		"sample_key",
		"insert-key",
		"insert_key",
		"paste-key",
		"paste_key",
		"changeme",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

// RedactSecrets removes complete credentials from evidence before reporting it.
func RedactSecrets(value string) string {
	for _, match := range findSecrets(value) {
		value = strings.ReplaceAll(value, match.Secret, RedactSecret(match.Secret))
	}
	return value
}

// RedactSecret preserves only a recognisable prefix and the final four characters.
func RedactSecret(secret string) string {
	if len(secret) <= 8 {
		return "****"
	}
	prefixLength := 4
	for _, prefix := range []string{"sk-ant-api03-", "sk-ant-", "sk-or-v1-", "sk-proj-", "sk-svcacct-", "sk-", "hf_", "AIza"} {
		if strings.HasPrefix(secret, prefix) {
			prefixLength = len(prefix)
			break
		}
	}
	if prefixLength+4 >= len(secret) {
		return "****" + secret[len(secret)-4:]
	}
	return secret[:prefixLength] + "****" + secret[len(secret)-4:]
}
