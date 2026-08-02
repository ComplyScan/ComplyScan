// Package governance creates reviewable governance-document scaffolds from a
// ComplyScan inventory. Generated documents deliberately avoid legal claims.
package governance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/1eonardodawinki/ComplyScan/internal/inventory"
)

const (
	DefaultAISystemPath       = "docs/AI_SYSTEM.md"
	DefaultRiskAssessmentPath = "docs/risk-assessment.md"
)

func AISystem(report inventory.Report, generatedAt time.Time) string {
	return fmt.Sprintf(`# AI system record

Status: Draft — human review required  
Generated: %s  
Inventory schema: %d  

This scaffold was generated from technical evidence detected by ComplyScan. Detection does not prove how a component is used, whether the system is in scope of a particular law, or whether it is compliant. Verify every entry and replace all TODOs before approval.

## Detected component inventory

%s

## Scan caveats

%s

## Purpose and intended use

- System or feature name: TODO
- Business purpose: TODO
- Intended users: TODO
- Intended decisions or outputs: TODO
- Prohibited or out-of-scope uses: TODO

## Ownership and accountability

- Business owner: TODO
- Technical owner: TODO
- Risk or compliance reviewer: TODO
- Approval date and approver: TODO
- Next review date: TODO

## Models, providers, and dependencies

For each detected component, confirm the exact model or service, hosting arrangement, version, contractual owner, and whether it is used at runtime, in tests, or only in configuration.

TODO: add reviewed model and provider details.

## Data flows

- Input data and sources: TODO
- Personal, confidential, or regulated data: TODO
- Data sent to third parties: TODO
- Storage locations and retention: TODO
- Training or provider reuse settings: TODO
- Output destinations and downstream decisions: TODO

## Human oversight and user experience

- Human review points: TODO
- User notices and AI disclosure: TODO
- Override, appeal, or escalation path: TODO
- Accessibility and affected-user considerations: TODO

## Controls and monitoring

- Access control and secret management: TODO
- Input and output safeguards: TODO
- Evaluation and acceptance criteria: TODO
- Production monitoring and alerting: TODO
- Incident response and rollback: TODO

## Evidence and linked records

- Risk assessment: TODO
- Privacy or data-protection assessment: TODO
- Security review: TODO
- Model or system evaluation results: TODO
- Vendor documentation and contracts: TODO

## Change and reassessment triggers

Review this record when a provider, model, intended use, data category, affected user group, deployment region, decision authority, or material safeguard changes.
`, generatedAt.UTC().Format("2006-01-02"), report.SchemaVersion, inventoryTable(report), warningList(report.Warnings))
}

func RiskAssessment(report inventory.Report, generatedAt time.Time) string {
	return fmt.Sprintf(`# AI risk assessment

Status: Draft — human review required  
Generated: %s  
Inventory schema: %d  

This scaffold is a technical governance aid, not a legal determination or compliance certificate. Confirm the system context, applicable obligations, and evidence with accountable reviewers.

## Assessment scope

- System or feature: TODO
- Assessment owner: TODO
- Technical reviewer: TODO
- Risk or legal reviewer: TODO
- Deployment regions: TODO
- Assessment date: TODO
- Next review date: TODO

## Detected component inventory

%s

## Scan caveats

%s

## Applicability and classification questions

- Does the feature make, recommend, rank, or materially influence decisions about people? TODO
- Is it used in employment, education, credit, insurance, healthcare, law enforcement, migration, essential services, or another sensitive domain? TODO
- Does it identify, categorise, profile, or infer traits about people? TODO
- Are users directly interacting with generated or manipulated content? TODO
- Is personal, confidential, copyrighted, or regulated data processed? TODO
- What laws, policies, contracts, and sector rules were reviewed? TODO
- Classification decision, rationale, reviewer, and date: TODO

## Risk register

| Risk area | Applicable? | Scenario and affected parties | Likelihood | Impact | Controls and evidence | Owner | Residual risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Privacy and data protection | TODO | TODO | TODO | TODO | TODO | TODO | TODO |
| Security and credential handling | TODO | TODO | TODO | TODO | TODO | TODO | TODO |
| Harmful or unsafe output | TODO | TODO | TODO | TODO | TODO | TODO | TODO |
| Bias, discrimination, and accessibility | TODO | TODO | TODO | TODO | TODO | TODO | TODO |
| Accuracy, hallucination, and over-reliance | TODO | TODO | TODO | TODO | TODO | TODO | TODO |
| Human oversight and contestability | TODO | TODO | TODO | TODO | TODO | TODO | TODO |
| Transparency and user expectations | TODO | TODO | TODO | TODO | TODO | TODO | TODO |
| Provider, model, and supply-chain dependency | TODO | TODO | TODO | TODO | TODO | TODO | TODO |
| Monitoring, incidents, and change control | TODO | TODO | TODO | TODO | TODO | TODO | TODO |

## Evaluation and acceptance criteria

- Evaluation datasets and their representativeness: TODO
- Quality, safety, fairness, privacy, and security metrics: TODO
- Acceptance thresholds and approver: TODO
- Known limitations and excluded scenarios: TODO
- Production monitoring thresholds: TODO

## Decision and approvals

- Decision: TODO — approve, approve with conditions, remediate, or do not deploy
- Required actions and due dates: TODO
- Accepted residual risks and accountable owner: TODO
- Approval evidence: TODO

## Reassessment triggers

Reassess after material changes to models, providers, data, intended use, affected groups, deployment regions, autonomy, safeguards, evaluation results, incidents, or applicable requirements.
`, generatedAt.UTC().Format("2006-01-02"), report.SchemaVersion, inventoryTable(report), warningList(report.Warnings))
}

// Write creates a generated document and protects existing review work unless
// force is explicitly requested.
func Write(path string, content string, force bool) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create document directory %q: %w", directory, err)
	}
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
		return fmt.Errorf("create document %q: %w", path, err)
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write document %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close document %q: %w", path, err)
	}
	return nil
}

func inventoryTable(report inventory.Report) string {
	if len(report.Components) == 0 {
		return "No supported AI provider or framework signals were detected. This does not establish that the repository contains no AI functionality; record manual review and unsupported components here."
	}
	var builder strings.Builder
	builder.WriteString("| Component | Kind | Confidence | Scope | Evidence | Packages | Representative locations |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, component := range report.Components {
		fmt.Fprintf(&builder, "| %s | %s | %s | %s | %s | %s | %s |\n",
			markdownCell(component.Name), markdownCell(string(component.Kind)), markdownCell(component.Confidence),
			markdownCell(scopeList(component.Scopes)), markdownCell(evidenceList(component.EvidenceTypes)),
			markdownCell(packageList(component.Packages)), markdownCell(locationList(component.Locations, 5)))
	}
	return strings.TrimRight(builder.String(), "\n")
}

func warningList(warnings []string) string {
	if len(warnings) == 0 {
		return "No discovery warnings were reported."
	}
	items := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		items = append(items, "- "+strings.ReplaceAll(warning, "\n", " "))
	}
	return strings.Join(items, "\n")
}

func scopeList(values []inventory.Scope) string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		items = append(items, string(value))
	}
	return strings.Join(items, ", ")
}

func evidenceList(values []inventory.EvidenceType) string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		items = append(items, string(value))
	}
	return strings.Join(items, ", ")
}

func packageList(values []inventory.PackageRef) string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		item := value.Name
		if value.Version != "" {
			item += " " + value.Version
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return "—"
	}
	sort.Strings(items)
	return strings.Join(items, ", ")
}

func locationList(values []inventory.Location, limit int) string {
	items := make([]string, 0, len(values))
	for index, value := range values {
		if index == limit {
			items = append(items, fmt.Sprintf("+%d more", len(values)-limit))
			break
		}
		item := value.Path
		if value.Line > 0 {
			item += fmt.Sprintf(":%d", value.Line)
		}
		items = append(items, item)
	}
	return strings.Join(items, ", ")
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.ReplaceAll(value, "\n", " ")
}
