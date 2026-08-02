# ComplyScan technical risk assessment

| Field | Value |
| --- | --- |
| Product | ComplyScan |
| Version assessed | 0.2.0 |
| Assessment date | 2026-08-02 |
| Owner | ComplyScan maintainers |
| Status | Initial maintained assessment |
| Next review | Before any material product change, or by 2027-02-02 |

## Scope and method

This assessment covers the deterministic offline CLI, its reports, baseline and suppression workflow, release artifacts, and optional GitHub Action. It considers foreseeable technical misuse and harm to developers, repository owners, people represented in source data, and downstream compliance decision-makers.

Likelihood and impact are qualitative: low, medium, or high. Residual risk reflects the current controls and does not imply that every deployment is safe or compliant.

## Risk register

| ID | Risk | Likelihood | Impact | Current controls | Residual risk |
| --- | --- | --- | --- | --- | --- |
| R-01 | A false negative gives unjustified confidence that relevant AI usage or risky code is absent. | Medium | High | Explicit non-certification language, typed technical signals, a labelled evaluation corpus with recall thresholds, visible rule inventory, and human-review remediation. | Medium |
| R-02 | A false positive wastes review effort or incorrectly implies a compliance concern. | Medium | Medium | Plain-name rejection, typed evidence, confidence labels, technical—not legal—claims, a labelled hard-negative corpus with precision thresholds, stable fingerprints, and reasoned suppressions. | Low |
| R-03 | Users treat a clear scan or generated report as legal advice, certification, or a complete EU AI Act assessment. | Medium | High | README and finding disclaimers, narrowly stated intended purpose, generated-document draft labels and TODOs, and repository-evidence prompts rather than legal conclusions. | Medium |
| R-04 | A real credential is exposed while scanning or reporting. | Low | High | Offline processing, no telemetry, redaction before evidence output, environment-reference exclusions, synthetic test credentials, and secret-rule regression tests. | Low |
| R-05 | Source content or sensitive metadata is disclosed through structured output or GitHub code scanning. | Low | High | Local CLI by default, short sanitised JSON evidence, source-free baselines, SARIF without evidence excerpts, explicit GitHub upload opt-out, and documented metadata fields. | Low |
| R-06 | A very large or adversarial repository exhausts memory, CPU, or scan time. | Medium | Medium | File-size, file-count, and total-byte budgets; binary and symlink exclusion; ignore processing; nested-repository boundaries; cancellation; and progress reporting. | Low |
| R-07 | Nested repositories, ignored files, untracked files, configured exclusions, or a changed-since reference create an incomplete scan boundary. | Medium | Medium | Warnings for skipped nested repositories and limits, documented defaults, `--include-nested-repositories`, `--tracked-only`, repeatable exclusions, full local-change inclusion, and repository-wide governance checks during changed-since scans. | Medium |
| R-08 | A broad suppression or stale baseline hides a finding that should be reviewed again. | Medium | Medium | Mandatory suppression reasons, exact stable fingerprints, source-free versioned baselines, visible suppressed counts, and `--no-baseline` review mode. | Medium |
| R-09 | A malicious contribution or compromised dependency alters scan behavior or release artifacts. | Low | High | Code review, CI tests and vetting, minimal dependencies, locked module checksums, cross-platform release automation, SHA-256 checksums, and GitHub build attestations. | Medium |
| R-10 | SARIF is rejected or produces unstable/duplicated code-scanning alerts. | Low | Medium | SARIF 2.1.0 output, stable partial fingerprints, relative paths, required source-location validation, and self-scan integration testing. | Low |
| R-11 | Findings change unexpectedly between platforms or runs. | Low | Medium | Repository-relative slash-separated paths, deterministic sorting, stable fingerprints, bounded discovery, reproducible release settings, and multi-platform CI/release builds. | Low |
| R-12 | A future model-backed provider introduces source disclosure, prompt injection, non-determinism, or incorrect generated conclusions. | Not active | High | Provider interfaces are inactive in 0.2.0; any activation is a mandatory applicability and risk reassessment trigger. | Not accepted until reassessed |

## Accepted limitations

ComplyScan 0.2.0 deliberately analyses repository evidence rather than a complete deployed system. It cannot observe runtime configuration, actual data subjects, model behaviour, organisational controls, intended use outside the repository, or downstream decisions. Its language and provider coverage is incomplete. These limitations are communicated to users and are not treated as defects that can be eliminated solely through more pattern rules.

## Verification evidence

The maintained verification baseline includes:

- unit and integration tests for discovery, rules, fingerprints, suppressions, baselines, reports, and CLI exit codes;
- labelled detector-corpus metrics with enforced precision and recall thresholds;
- changed-since tests covering committed, staged, unstaged, untracked, subdirectory, and repository-wide governance behavior;
- race-enabled tests and `go vet` before release;
- a self-scan that exercises the published composite action and GitHub SARIF upload;
- secret-redaction and false-positive regression tests;
- deterministic GoReleaser archives for macOS, Linux, and Windows on amd64 and arm64; and
- release checksums and attestations.

## Review and escalation

Material incidents, credible false-negative reports, credential disclosure, SARIF data leakage, dependency compromise, or any reassessment trigger in [AI_SYSTEM.md](AI_SYSTEM.md) requires maintainer review before the next release. Security issues should follow the private reporting process in [SECURITY.md](../SECURITY.md).

This document is an engineering risk assessment, not legal advice or a conformity assessment.
