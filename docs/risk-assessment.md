# ComplyScan technical risk assessment

| Field | Value |
| --- | --- |
| Product | ComplyScan |
| Version assessed | Unreleased 0.2.0 development branch |
| Assessment date | 2026-08-02 |
| Owner | ComplyScan maintainers |
| Status | Updated for optional Ollama integration; live-model validation and applicability review remain open before release |
| Next review | Before release, any material product change, or by 2027-02-02 |

## Scope and method

This assessment covers the deterministic offline-by-default CLI, optional Ollama advisory review, reports, baseline and suppression workflow, release artifacts, and optional GitHub Action. It considers foreseeable technical misuse and harm to developers, repository owners, people represented in source data, and downstream compliance decision-makers.

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
| R-12 | Ollama produces incorrect, inconsistent, overconfident, or legally framed observations that users mistake for authoritative findings. | Medium | High | Explicit opt-in, advisory-only data model, separate report section, fixed non-certification prompt, schema-constrained output, temperature zero, strict enums, no effect on deterministic findings or exit status, and human-review language. | Medium |
| R-13 | Repository-controlled finding text performs prompt injection or causes an observation to be attached to the wrong finding. | Medium | High | Only bounded finding records are sent; system and user prompts label every field untrusted; complete source files are excluded; fingerprints and rule IDs must exactly match submitted records; unknown, duplicate, or malformed observations fail review. | Medium |
| R-14 | Sensitive evidence is disclosed through Ollama, a proxy, redirect, remote endpoint, cloud-routed model, or generated rationale. | Low | High | Input and output re-redaction, length bounds, loopback-only endpoint validation, proxies and redirects disabled, no authentication fields, no complete files, and explicit operator warning that Ollama model acquisition or cloud routing is a separate boundary. | Low for ComplyScan transport; deployment-dependent overall |
| R-15 | Local inference hangs, consumes excessive resources, or blocks CI. | Medium | Medium | Configurable timeout, cancellation, maximum 100 and default 20 reviewed findings, non-streaming bounded responses, no model call when no findings exist, and explicit failure instead of silent partial review. | Low |
| R-16 | An incomplete, inaccurate, self-serving, or stale system profile produces misleading applicability screening. | Medium | High | Explicit `unknown` values, controlled enums, visible missing-context output, named/date-stamped confirmation, separate automated and human decisions, and no automated exemption or compliance verdict. Periodic staleness enforcement remains future work. | Medium |
| R-17 | Users put secrets, personal records, or confidential business details into the version-controlled system profile. | Low | High | Setup asks only for categories and short factual labels, warns against secrets and personal records, and documents the public/version-controlled boundary. Automated profile-content redaction is not yet implemented. | Medium |
| R-18 | A human-recorded applicability decision is mistaken for independent legal validation by ComplyScan. | Medium | High | Decisions require status, rationale, reviewer, and date; automated screening remains separate; terminal and JSON notes deny legal-determination and certification status. | Medium |
| R-19 | An incomplete, outdated, or incorrect control-pack mapping omits an applicable obligation or points users to the wrong evidence. | Medium | High | Pack ID, semantic version, release date, official source edition, SHA-256 content digest, explicit provision coverage, visible exclusions, strict schema validation, and primary-source review. The v0.1.0 pack is explicitly limited to Articles 9–15 for candidate high-risk providers. | Medium |
| R-20 | Keyword matching treats irrelevant documentation as evidence, misses differently worded evidence, or encourages checklist compliance. | High | High | Matches are labelled candidates; grouped terms and eligible file kinds reduce noise; no excerpts are retained; statuses stop at `evidence-found`; verification modes and missing objectives remain visible; framework gaps do not gate CI. Labelled control-evidence evaluation is still required. | Medium |
| R-21 | Framework JSON discloses sensitive repository structure through evidence paths and system-profile metadata. | Low | High | No source excerpts are included, paths are repository-relative, profile fields are bounded and reject line breaks, and local output is the default. Users are warned not to place secrets, personal records, or confidential case details in committed profiles. | Low |

## Accepted limitations

ComplyScan 0.2 development combines self-declared profile facts with repository evidence rather than observing a complete deployed system. It cannot verify that profile answers match real operations, runtime configuration, actual data subjects, organisational controls, intended use outside the repository, or downstream decisions. Ollama sees only finding records and therefore cannot resolve most missing system context. Its language, provider, and model-evaluation coverage is incomplete. These limitations are communicated to users and are not treated as defects that can be eliminated solely through more rules or model prompts.

## Verification evidence

The maintained verification baseline includes:

- unit and integration tests for discovery, rules, fingerprints, suppressions, baselines, reports, and CLI exit codes;
- labelled detector-corpus metrics with enforced precision and recall thresholds;
- changed-since tests covering committed, staged, unstaged, untracked, subdirectory, and repository-wide governance behavior;
- race-enabled tests and `go vet` before release;
- a self-scan that exercises the published composite action and GitHub SARIF upload;
- secret-redaction and false-positive regression tests;
- fake-transport Ollama tests covering structured requests, redaction, API errors, identifier binding, remote-endpoint rejection, and zero-finding behavior;
- profile validation and guided-setup tests covering explicit unknowns, attribution, duplicate IDs, conservative scope screening, existing-config updates, and report separation;
- embedded pack parsing, source/version/digest validation, role and applicability activation tests, bounded deterministic evidence matching, gap-report tests, and complete-repository checks for changed scans;
- deterministic GoReleaser archives for macOS, Linux, and Windows on amd64 and arm64; and
- release checksums and attestations.

Before release, verification must also include a smoke test against a supported locally installed Ollama model, review-output inspection using prompt-injection fixtures, and resource measurements on a representative repository. Automated tests do not currently download or execute an Ollama model.

## Review and escalation

Material incidents, credible false-negative reports, credential disclosure, SARIF data leakage, dependency compromise, or any reassessment trigger in [AI_SYSTEM.md](AI_SYSTEM.md) requires maintainer review before the next release. Security issues should follow the private reporting process in [SECURITY.md](../SECURITY.md).

This document is an engineering risk assessment, not legal advice or a conformity assessment.
