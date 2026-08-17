package reviewcontext

import (
	"fmt"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/rules"
	"github.com/ComplyScan/ComplyScan/internal/verification"
)

const maxVerificationContextChars = 6_000

// AttachVerifications adds bounded, already-redacted isolated test results to
// matching objective candidates. The user-declared association remains visible
// and the result is evidence, never an instruction or a compliance decision.
func AttachVerifications(request providers.TechnicalReviewRequest, results []verification.Report) providers.TechnicalReviewRequest {
	for candidateIndex := range request.Candidates {
		candidate := &request.Candidates[candidateIndex]
		var source strings.Builder
		for _, result := range results {
			if !verificationCoversObjective(result, candidate.ObjectiveID) || !verificationCoversSystem(result, candidate.SystemID) {
				continue
			}
			if source.Len() > 0 {
				source.WriteString("\n---\n")
			}
			fmt.Fprintf(&source, "Recipe: %s\nUser-declared objective: %s\nDeclared systems: %s\nStatus: %s\nExit code: %d\nCommand: %s\nDuration milliseconds: %d\nOutput digest: %s\n",
				result.RecipeID, candidate.ObjectiveID, strings.Join(result.Systems, ", "), result.Status,
				result.ExitCode, strings.Join(result.Command, " "), result.DurationMS, result.OutputDigest)
			if result.Output != "" {
				source.WriteString("Bounded redacted output:\n")
				source.WriteString(result.Output)
				source.WriteByte('\n')
			}
		}
		// Verification output is already redacted by the runner, but apply the
		// same canonical defence at the context boundary so recipe metadata and
		// future result producers cannot introduce a credential.
		value := []rune(rules.RedactSecrets(source.String()))
		if len(value) == 0 {
			continue
		}
		if len(value) > maxVerificationContextChars {
			value = append(value[:maxVerificationContextChars-1], '…')
		}
		candidate.SourceContexts = append(candidate.SourceContexts, providers.TechnicalSourceContext{
			Role: "isolated-verification-result", Symbol: "user-declared-objective-tests",
			Path: "(isolated-verification)", Source: string(value),
		})
	}
	return request
}

func verificationCoversSystem(result verification.Report, systemID string) bool {
	if systemID == "" {
		return true
	}
	for _, system := range result.Systems {
		if system == systemID {
			return true
		}
	}
	return false
}

func verificationCoversObjective(result verification.Report, objectiveID string) bool {
	for _, objective := range result.Objectives {
		if objective == objectiveID {
			return true
		}
	}
	return false
}
