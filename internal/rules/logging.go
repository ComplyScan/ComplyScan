package rules

import (
	"context"
	"regexp"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

type PromptLoggingRule struct{}

var (
	loggingCallPattern = regexp.MustCompile(`(?i)(log\.(?:print|printf|println)|fmt\.(?:print|printf|println)|logging\.(?:debug|info|warning|error|critical)|logger\.(?:trace|debug|info|warn|warning|error|critical)|console\.(?:log|debug|info|warn|error))\s*\(`)
	loggedValuePattern = regexp.MustCompile(`(?i)\b(prompt|user_?prompt|userPrompt|message|messages|response|completion|model_?output|modelOutput|chat_?history|chatHistory|conversation)\b`)
)

func (PromptLoggingRule) ID() string { return "AI-LOG-001" }

func (rule PromptLoggingRule) Run(ctx context.Context, repo discovery.Repository) ([]Finding, error) {
	return collectFindings(func(emit FindingEmitter) error {
		return rule.RunStreaming(ctx, repo, emit)
	})
}

func (PromptLoggingRule) RunStreaming(ctx context.Context, repo discovery.Repository, emit FindingEmitter) error {
	for _, file := range repo.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if file.Kind != discovery.KindSource {
			continue
		}
		for lineIndex, line := range lines(file.Content) {
			if !loggingCallPattern.MatchString(line) {
				continue
			}
			codeWithoutStrings := stripQuotedStrings(line)
			match := loggedValuePattern.FindString(codeWithoutStrings)
			if match == "" {
				continue
			}
			confidence := "medium"
			normalized := strings.ToLower(match)
			if normalized == "message" || normalized == "messages" || normalized == "response" {
				confidence = "low"
			}
			if err := emit(Finding{
				RuleID: "AI-LOG-001", Title: "Prompt or model response may be logged",
				Severity: SeverityHigh, Category: "data-handling",
				Message:     "A value named " + match + " appears to be passed to a logging function. Prompts and model outputs may contain personal or sensitive information and require human review.",
				Path:        file.Path,
				StartLine:   lineIndex + 1,
				EndLine:     lineIndex + 1,
				Evidence:    sanitizeEvidence(line, 160),
				Remediation: "Remove the value from logs or apply documented minimisation, redaction, access controls, and retention limits.",
				Confidence:  confidence,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func stripQuotedStrings(value string) string {
	masked := []byte(value)
	var quote byte
	for index := 0; index < len(masked); index++ {
		character := masked[index]
		if quote == 0 {
			if character == '\'' || character == '"' || character == '`' {
				quote = character
				masked[index] = ' '
			}
			continue
		}
		masked[index] = ' '
		if character == '\\' && index+1 < len(masked) {
			index++
			masked[index] = ' '
			continue
		}
		if character == quote {
			quote = 0
		}
	}
	return string(masked)
}
