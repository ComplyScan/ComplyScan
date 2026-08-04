# Changelog

## Unreleased

### Added

- `complyscan doctor` for offline build, repository, configuration, report-permission, Git, and optional Ollama readiness checks.
- A repeatable `qwen3:8b` live-validation harness with enforced production/test-only expectations and saved resource metrics.
- Go, Python, JavaScript, and TypeScript repository-graph context for technical evidence, including routes, authorization, persistence, logging, configuration, and production/test reachability.
- A versioned technical-evidence benchmark with labelled multi-language repository cases, hard negatives, machine-readable metrics, and CI acceptance thresholds.
- A source-free external benchmark that verifies and scans exact commits of three permissively licensed public AI repositories, with recorded provenance and human candidate labels.

### Changed

- The public Go module and source references now use `github.com/ComplyScan/ComplyScan`.
- CI now verifies the documented Go 1.22 minimum.
- Technical evidence now uses bounded keyword boundaries and local line context, preserves camel-case path signals, and applies the refined EU AI Act technical pack `0.1.1` terms derived from public-repository false-positive review.

### Testing

- Installer regressions cover unsupported systems, incomplete releases, and preservation of existing binaries after failed updates.
- Offline Ollama tests cover prompt injection, malformed output, invalid verdicts, duplicate bindings, and timeouts without downloading a model.
- Technical-evidence regression metrics cover candidate precision and recall, anchor and reachability accuracy, relationship recall, forbidden relationships, and language coverage.

## 0.1.2 - 2026-08-03

### Added

- Aggregated AI provider and framework inventory with representative locations.
- Bounded discovery, nested-repository boundaries, Git-tracked-only scans, exclusions, and terminal progress.
- Stable finding fingerprints, reasoned suppressions, and source-free baselines.
- SARIF 2.1.0 output and a reusable GitHub code-scanning action.
- Cross-platform release archives, SHA-256 checksums, and build attestations.
- Maintained AI applicability and technical risk assessments for ComplyScan itself.
- Typed AI-component signals for recognised dependencies, imports, endpoints, and environment-variable access.
- A labelled positive and hard-negative evaluation corpus with enforced precision and recall thresholds.
- A structured `inventory` command with terminal and versioned JSON reports.
- `generate ai-system` and `generate risk-assessment` commands that create reviewable, inventory-prefilled governance scaffolds.
- `scan --changed-since <git-ref>` and a matching GitHub Action input for pull-request scope, including local staged, unstaged, and untracked files.
- Explicitly enabled, loopback-only Ollama review of sanitised deterministic findings using schema-constrained output.
- Advisory observations in terminal, JSON, and SARIF without changing deterministic findings or exit status.
- Guided multi-system profiles covering intended purpose, regions, roles, use-case domains, people, data, deployment, oversight, and attributable human applicability decisions.
- Conservative EU AI Act scope and possible high-risk screening in `profile show`, terminal scans, and JSON reports.
- `profile setup` for adding or replacing system context in an existing configuration.
- A content-addressed `eu-ai-act-technical-evidence` v0.1.0 pack with 13 code-only objectives associated with Articles 9, 10, 12, 14, 15, and 50.
- `framework list` and profile-independent `framework assess` commands with terminal and versioned JSON evidence reports.
- Conservative `candidate-evidence`, `not-detected`, and `not-evaluated` objective statuses without control-level compliance rollups.
- Versioned scan identity, timestamp, build identity, and explicit finding/evidence scope in JSON bundles.
- Automatic atomic `.complyscan/reports/latest.md` and `latest.json` output, with `--no-report` and safe target-relative `--report-dir` overrides.
- Git-ignore initialization and built-in discovery exclusion for generated scan reports.
- A unified `setup` wizard for system context, human applicability decisions, local model selection, consent-based Ollama provisioning, and an optional first scan.
- A one-command macOS/Linux installer with platform detection, mandatory release checksum verification, user-local installation, pinned versions, and automatic guided-setup handoff.
- A minimal GitHub Pages deployment that publishes the installer at `complyscan.github.io/ComplyScan/install.sh` without exposing the repository as site content.

### Changed

- Terminal findings are emitted live while rules run.
- Scan-wide AI analysis is shared across rules to avoid repeated repository work.
- AI discovery now requires technical evidence instead of matching plain provider names.
- Changed-since scans keep documentation and risk-evidence checks repository-wide while scoping code rules to changed files.
- Forced configuration updates are validated and written atomically while preserving file permissions.
- Changed-since scans retain the complete repository snapshot for technical and governance evidence while recording the narrower finding scope.

### Fixed

- Public repository references and installation flow.
- Secret detection no longer treats ordinary hyphenated words ending in `sk-...` as credentials.
- ComplyScan's detector signatures and synthetic fixtures no longer appear as AI components in its self-inventory.
