package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

const maxActionHistoryReportBytes = 64 << 20

const developerActionVerificationCommand = "complyscan scan"

// EnsureDeveloperActions derives the current action plan when a caller has not
// already attached lifecycle-aware actions. Keeping this at the report boundary
// ensures JSON, Markdown, SARIF, and future agent tools share one action model.
func EnsureDeveloperActions(value Report) Report {
	derived := CurrentDeveloperActions(value)
	if len(value.DeveloperActions) == 0 {
		value.DeveloperActions = derived
		return value
	}

	existing := make(map[string]DeveloperAction, len(value.DeveloperActions))
	for _, action := range value.DeveloperActions {
		existing[action.ID] = action
	}
	for index := range derived {
		prior, ok := existing[derived[index].ID]
		if !ok {
			continue
		}
		if prior.Status != "" {
			derived[index].Status = prior.Status
		}
		if prior.FirstSeenScanID != "" {
			derived[index].FirstSeenScanID = prior.FirstSeenScanID
		}
	}
	for _, prior := range value.DeveloperActions {
		if _, active := developerActionByID(derived, prior.ID); !active {
			derived = append(derived, prior)
		}
	}
	value.DeveloperActions = derived
	return value
}

// ReconcileDeveloperActionLifecycle compares a completed scan with the prior
// local evidence bundle. Missing actions are resolved only when the current
// scan had enough coverage to make that conclusion safely.
func ReconcileDeveloperActionLifecycle(value Report, previous *Report) Report {
	return ReconcileDeveloperActionLifecycleWithOptions(value, previous, DeveloperActionLifecycleOptions{})
}

type DeveloperActionLifecycleOptions struct {
	// ChangedPaths is set only when previous is an explicit full baseline for
	// this change. It permits safe resolution of a prior deterministic finding
	// when every cited file was part of the reviewed change.
	ChangedPaths map[string]struct{}
}

func ReconcileDeveloperActionLifecycleWithOptions(value Report, previous *Report, options DeveloperActionLifecycleOptions) Report {
	value.DeveloperActions = nil
	value = EnsureDeveloperActions(value)
	if previous == nil || len(previous.DeveloperActions) == 0 {
		return value
	}

	priorByID := make(map[string]DeveloperAction, len(previous.DeveloperActions))
	for _, action := range previous.DeveloperActions {
		priorByID[action.ID] = action
	}
	currentByID := make(map[string]struct{}, len(value.DeveloperActions))
	for index := range value.DeveloperActions {
		action := &value.DeveloperActions[index]
		currentByID[action.ID] = struct{}{}
		prior, existed := priorByID[action.ID]
		if !existed {
			continue
		}
		action.FirstSeenScanID = prior.FirstSeenScanID
		if action.FirstSeenScanID == "" {
			action.FirstSeenScanID = previous.Scan.ID
		}
		action.LastSeenScanID = value.Scan.ID
		switch prior.Status {
		case DeveloperActionResolved:
			action.Status = DeveloperActionReopened
		case DeveloperActionAccepted:
			action.Status = DeveloperActionAccepted
		default:
			action.Status = DeveloperActionOpen
		}
	}
	for _, prior := range previous.DeveloperActions {
		if _, exists := currentByID[prior.ID]; exists {
			continue
		}
		if prior.Status == DeveloperActionResolved {
			value.DeveloperActions = append(value.DeveloperActions, prior)
			continue
		}
		if developerActionCanResolve(prior, value, options) {
			prior.Status = DeveloperActionResolved
			prior.ResolvedInScanID = value.Scan.ID
			value.DeveloperActions = append(value.DeveloperActions, prior)
			continue
		}
		// A partial, changed-scope, or model-free run cannot prove that a
		// previously observed action disappeared. Retain it honestly.
		value.DeveloperActions = append(value.DeveloperActions, prior)
	}
	return value
}

func developerActionCanResolve(action DeveloperAction, value Report, options DeveloperActionLifecycleOptions) bool {
	fullRepository := strings.TrimSpace(value.Scan.Scope.ChangedSince) == ""
	switch action.Category {
	case "deterministic-finding":
		return fullRepository || developerActionEvidenceChanged(action, options.ChangedPaths)
	case "human-context", "verification":
		return fullRepository
	case "scan-operation":
		return len(value.Warnings) == 0 && value.RepositoryAnalysisRun != RepositoryAnalysisIncomplete
	case "code-control":
		return fullRepository && value.RepositoryAnalysisRun == RepositoryAnalysisCompleted
	default:
		return false
	}
}

func developerActionEvidenceChanged(action DeveloperAction, changed map[string]struct{}) bool {
	if len(changed) == 0 || len(action.Evidence) == 0 {
		return false
	}
	seenPath := false
	for _, evidence := range action.Evidence {
		path := strings.TrimSpace(strings.ReplaceAll(evidence.Path, "\\", "/"))
		if path == "" {
			continue
		}
		seenPath = true
		if _, ok := changed[path]; !ok {
			return false
		}
	}
	return seenPath
}

// ReadJSONFile loads a bounded local report for action history and agent tools.
func ReadJSONFile(path string) (Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return Report{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Report{}, fmt.Errorf("inspect ComplyScan report %q: %w", path, err)
	}
	if info.Size() > maxActionHistoryReportBytes {
		return Report{}, fmt.Errorf("ComplyScan report %q exceeds %d bytes", path, maxActionHistoryReportBytes)
	}
	decoder := json.NewDecoder(file)
	var value Report
	if err := decoder.Decode(&value); err != nil {
		return Report{}, fmt.Errorf("decode ComplyScan report %q: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Report{}, fmt.Errorf("decode ComplyScan report %q: unexpected trailing JSON value", path)
		}
		return Report{}, fmt.Errorf("decode ComplyScan report %q: %w", path, err)
	}
	if value.SchemaVersion < 6 || value.SchemaVersion > 17 {
		return Report{}, fmt.Errorf("ComplyScan report %q uses unsupported schema version %d", path, value.SchemaVersion)
	}
	return value, nil
}

// CurrentDeveloperActions returns every actionable item for this scan, not
// just the three items shown in the concise Markdown report.
func CurrentDeveloperActions(value Report) []DeveloperAction {
	view := buildDeveloperReportView(value, "latest.json")
	result := make([]DeveloperAction, 0, len(view.allActions))
	for _, action := range view.allActions {
		evidence := append([]DeveloperActionEvidence(nil), action.locations...)
		if len(evidence) == 0 && strings.TrimSpace(action.evidence) != "" {
			evidence = append(evidence, DeveloperActionEvidence{Summary: strings.TrimSpace(action.evidence)})
		}
		if len(evidence) == 0 {
			evidence = append(evidence, DeveloperActionEvidence{Summary: developerActionEvidenceSummary(action)})
		}
		verification := DeveloperActionVerification{
			Type: "rescan", Description: "Run ComplyScan again and confirm that this action is no longer open.",
			Command: developerActionVerificationCommand,
		}
		if action.humanOnly {
			verification = DeveloperActionVerification{Type: "human-review", Description: "A developer or compliance owner must record the missing product or organisation context."}
		}
		status := DeveloperActionNew
		result = append(result, DeveloperAction{
			ID: action.id, Status: status, Priority: action.priority, Category: action.category,
			Title: developerPlainLanguage(action.issue), Why: developerPlainLanguage(action.why), RecommendedChange: developerPlainLanguage(action.next),
			AcceptanceCriteria: developerActionAcceptanceCriteria(action), Evidence: evidence,
			Frameworks:   append([]DeveloperActionFramework(nil), action.frameworks...),
			Verification: verification,
			HumanOnly:    action.humanOnly, FirstSeenScanID: value.Scan.ID, LastSeenScanID: value.Scan.ID,
		})
	}
	if result == nil {
		return []DeveloperAction{}
	}
	return result
}

func developerActionEvidenceSummary(action developerAction) string {
	switch action.category {
	case "code-control":
		return "The reviewed repository scope did not provide sufficient matching code evidence for this safeguard."
	case "scan-operation":
		return "The scan warning or incomplete lifecycle is recorded in this evidence bundle."
	case "human-context":
		return "Repository code cannot establish the requested product or organisation context."
	default:
		return "This action is derived from the checked report evidence and coverage state."
	}
}

func developerActionAcceptanceCriteria(action developerAction) []string {
	criteria := make([]string, 0, 2)
	switch action.category {
	case "verification":
		criteria = append(criteria, "The configured execution check completes successfully.")
	case "scan-operation":
		criteria = append(criteria, "The scan completes without this warning or incomplete state.")
	case "human-context":
		criteria = append(criteria, "A developer or compliance owner reviews and records the requested context.")
	default:
		criteria = append(criteria, "The recommended code or configuration change is implemented and supported by repository evidence.")
	}
	criteria = append(criteria, "A fresh ComplyScan scan no longer reports this action as open.")
	return criteria
}

func developerActionByID(actions []DeveloperAction, id string) (DeveloperAction, bool) {
	for _, action := range actions {
		if action.ID == id {
			return action, true
		}
	}
	return DeveloperAction{}, false
}

func developerFindingActionEvidence(finding rules.Finding) []DeveloperActionEvidence {
	locations := make([]DeveloperActionEvidence, 0, 1+len(finding.Locations))
	if strings.TrimSpace(finding.Path) != "" {
		locations = append(locations, DeveloperActionEvidence{
			Path: finding.Path, StartLine: finding.StartLine, EndLine: finding.EndLine, Summary: finding.Evidence,
		})
	}
	for _, location := range finding.Locations {
		locations = append(locations, DeveloperActionEvidence{
			Path: location.Path, StartLine: location.StartLine, EndLine: location.EndLine, Summary: location.Evidence,
		})
	}
	return locations
}

func developerReferenceActionEvidence(references []reconciliation.EvidenceReference) []DeveloperActionEvidence {
	result := make([]DeveloperActionEvidence, 0, len(references))
	for _, reference := range references {
		result = append(result, DeveloperActionEvidence{Path: reference.Path, StartLine: reference.Line})
	}
	return result
}

func developerCitationActionEvidence(citations []providers.RepositoryCitation) []DeveloperActionEvidence {
	result := make([]DeveloperActionEvidence, 0, len(citations))
	for _, citation := range citations {
		result = append(result, DeveloperActionEvidence{Path: citation.Path, StartLine: citation.Line, Summary: citation.Summary})
	}
	return result
}
