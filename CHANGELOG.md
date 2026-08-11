# Changelog

## Unreleased

### Changed

- Interactive setup, profile, ownership, and verification wizards now render their main section titles as consistent bold terminal headings while preserving plain output for accessible, dumb, redirected, and `NO_COLOR` modes.
- Guided setup now shows five numbered stages from privacy selection through review and first scan, so users can see where they are and how much remains.
- Local model selection now shows transparent download/runtime-memory ranges, model pulls retain Ollama's live progress and report elapsed time, and qualification, profile drafting, finding review, and framework evidence investigation report elapsed and target progress with the actual provider name.
- Guided setup now presents a final summary of analysis privacy, model readiness, frameworks, systems, evidence ownership, and first-run behavior before writing configuration; users can revisit any major section or cancel without saving.

## 0.1.6 - 2026-08-11

### Added

- Automatic one-request model qualification for arbitrary Ollama tags and supported BYOK model IDs, with no repository data, strict structured-record binding, an instruction-shaped hard negative, a private 30-day cache keyed by model/digest and prompt contracts, setup/deep-scan enforcement, doctor status, and deterministic fallback.

### Changed

- Guided setup now uses keyboard-controlled radio menus for fixed single choices, checkboxes for multiple choices, highlighted Yes/No confirmations, and selectable local or remote model lists with an explicit custom-model entry. This also covers system, test-command, and objective selection in verification setup; redirected input, dumb terminals, and `COMPLYSCAN_ACCESSIBLE=1` retain the stable numbered or text fallback.
- First-run setup now establishes its local, cloud, or model-free privacy boundary before repository analysis, uses a ready reviewer to draft only repository-evident profile answers with confidence and cited paths, and presents every draft as an editable human-confirmed default. Jurisdiction, organisation role, actual production use, legal applicability, negative data claims, and other facts that source code cannot establish remain direct human questions.
- The final first-run scan reuses the bounded repository discovery already completed during setup rather than traversing and classifying the same files a second time; code-graph construction and framework evaluation still run once against the confirmed profile.

### Testing

- A labelled AI-assisted-onboarding benchmark measures controlled-field precision and recall, forbidden inferences, grounded citations, and per-case time across three positive repository shapes and a documentation-only hard negative. A live Ollama harness saves machine-readable results and resource measurements without making model execution part of CI.
- Profile-draft prompt version 4 passed the complete `qwen3.5:9b` onboarding gate with 88.2% precision, 88.2% recall, zero forbidden claims, zero ungrounded references, and all four cases within their time limit; the separate technical-review gate remains pending.

## 0.1.5 - 2026-08-10

### Changed

- The recommended Ollama default is now `qwen3.5:9b`; `qwen3:8b` remains selectable as the previously validated model, and the new default is explicitly marked as pending the maintained live quality gate.
- Interactive setup now uses numbered menus for framework, profile, review-provider, ownership, and verification choices; multi-select questions accept comma-separated numbers instead of requiring internal values.
- First-run setup now inspects and summarizes the repository before asking questions, recommends a technical framework mapping from declared regions, and follows the short screening with only the additional EU applicability questions made relevant by earlier answers. The complete unbranched questionnaire and attributed legal decision record remain available through `complyscan setup --advanced`.
- EU reports now label technical mapping context as `incomplete`, `factually-ready`, or `human-reviewed`, list unresolved facts, and keep requirement mappings explicitly provisional when inputs are missing.
- First-run setup now offers an explicit quick scan, deep AI review, or configure-only choice. `complyscan scan --quick` guarantees a model-free preliminary scan, while `--deep` requires a configured review provider.
- Running `complyscan` without a subcommand now starts discovery-first setup when no repository configuration exists and scans immediately when the current repository is already configured.
- Deep scans now save a complete preliminary Markdown and JSON report before the first model request, checkpoint completed framework reviews, and preserve deterministic results when advisory finding review is unavailable.
- A failed or invalid model response now marks only that technical target as incomplete and continues with the remaining evidence investigations instead of aborting the entire scan.
- Deep review now reuses deterministic graph and source retrieval directly, asks the model to plan follow-up searches only for context-poor investigations, and bounds local-model output budgets to reduce unnecessary generation time.
- Advisory review now inspects at most two representative code candidates per system and technical objective, while retaining every deterministic match in the report and disclosing any omitted repetitive model targets.
- Terminal scans now finish with a concise AI inventory, technical-objective, requirement-mapping, and advisory-review summary; `--verbose` restores the full framework and evidence dump while reports always retain complete detail.
- Findings now identify production, test, documentation/example, or configuration scope. Ambiguous generic `message` logging signals are medium severity, while explicit prompt, response, or model-output logging remains high severity, and additional obvious token placeholders are ignored.
- Routine `complyscan` and `complyscan scan` runs are now deterministic by default even when a reviewer is configured; model execution requires `--deep` or an explicit `--review` override.

## 0.1.4 - 2026-08-08

### Added

- A built-in, code-only NIST AI RMF 1.0 technical evidence pack with 11 inspectable objectives and explicit voluntary-framework semantics.
- Framework selection in configuration and guided setup, including repeatable `--framework` automation flags and EU-specific applicability questions only when the EU pack is selected.
- Canonical technical control IDs that let different framework objectives reuse repository evidence and stable fingerprints without conflating their citations or legal nature.
- Per-framework terminal, Markdown, and schema-version 5 JSON results, plus selected-framework objective choices in guided verification setup.
- Validated, version-controlled path ownership rules that assign repository evidence to one declared AI system or intentionally share it across several systems.
- Guided `complyscan ownership setup` and inspectable `complyscan ownership show` commands, also offered during multi-system setup.
- Per-reference ownership status in terminal, Markdown, and schema-version 5 JSON reports, including assigned, shared, conflicting, unassigned, and disclosed single-system inference states.

### Changed

- Scans now discover and inventory a repository once, then evaluate every configured technical pack and label NIST results as voluntary recommendations rather than likely legal requirements.
- The EU AI Act technical pack is version 0.1.3 with schema-version 2 applicability fields and canonical control mappings; its measured evidence-match terms remain unchanged.
- The technical-review prompt contract is version 10 and binds review targets to their selected framework pack.
- Reconciliation now maps objective evidence and observed AI components only to systems that own the matching repository path; conflicting and unmatched paths remain explicitly unresolved.
- Advisory candidate and missing-evidence investigations are now scoped per system. Graph construction, initial retrieval, model-directed follow-up, cache entries, observations, isolated-test context, and reconciliation use only assigned or intentionally shared paths.
- The source-free cache uses schema version 2 so earlier unscoped observations cannot be reused.

### Testing

- Multi-framework regressions cover configuration validation, setup branching, report rendering, recommendation semantics, shared-control fingerprints, and verification objective selection.
- A source-free NIST synthetic benchmark covers all 11 objectives, four indexed-language case types, production and test-only reachability, required graph relationships, and split-keyword hard negatives with enforced CI thresholds.
- Independently reviewed NIST labels over ten exact public-repository revisions record 32 reasonable candidates, five false positives, 86.5% candidate precision, and complete recall against the labelled paths without storing third-party source.
- A cache-bypassed prompt-version 10 `qwen3:8b` gate passed across Go, Python, and TypeScript with independently validated EU and NIST targets, bounded prompt-injection fixtures, and no semantic guardrail corrections; a controlled awake TypeScript rerun completed in 592.9 seconds.

## 0.1.3 - 2026-08-06

### Added

- `complyscan doctor` for offline build, repository, configuration, report-permission, Git, and optional Ollama readiness checks.
- A repeatable `qwen3:8b` live-validation harness with enforced production/test-only expectations and saved resource metrics.
- Go, Python, JavaScript, and TypeScript repository-graph context for technical evidence, including routes, authorization, persistence, logging, configuration, and production/test reachability.
- A versioned technical-evidence benchmark with labelled multi-language repository cases, hard negatives, machine-readable metrics, and CI acceptance thresholds.
- A source-free external benchmark that verifies and scans exact commits of three permissively licensed public AI repositories, with recorded provenance and human candidate labels.
- An opt-in `qwen3:8b` semantic benchmark over the pinned public-repository candidates, with source-free decision output, explicit acceptance policy, quality thresholds, and focused candidate debugging.
- A private, source-context-free technical-review cache in the operating system's user-cache directory, with content-aware invalidation, atomic writes, and `--refresh-review` bypass support.
- Live per-candidate technical-review progress that distinguishes Ollama inference from cache reuse.
- Explicit system activity declarations for inference, training, fine-tuning, evaluation, automated decisions, agent tool use, and synthetic-content generation.
- Versioned deterministic reconciliation between screened EU AI Act technical objectives and independently discovered repository evidence, including visible non-detections, mismatches, and unassigned evidence.
- Developer-focused explanations, category definitions, and examples before every interactive setup question, including a safe `needs-review` default for human applicability.
- EU AI Act technical pack `0.1.2` with strictly validated, inspectable applicability scope, activity, and external-use conditions beside every code-evidence objective.
- Bounded model investigation of technical candidates and likely-required objectives where deterministic evidence was not detected, including one validated follow-up retrieval round over eligible repository files.
- Source-grounded model conclusions, supporting and contradicting evidence, unresolved questions, deterministic semantic guardrails, and separately reported evidence assurance.
- Opt-in isolated Docker or Podman verification that runs explicitly configured tests without a shell, network, writable repository mount, or elevated container privileges.
- Reusable verification recipes and guided setup that connect bounded test results to selected technical objectives without turning those results into legal conclusions.
- An Ollama model picker that lists installed and recommended models while accepting any exact local model tag.
- BYOK OpenAI, Anthropic, and Gemini model review through fixed official endpoints, schema-constrained output, explicit remote-processing consent, and environment-only credentials.
- Remote-provider readiness checks and an explicit `doctor --probe-review` synthetic compatibility request.
- GitHub Action inputs for local or BYOK model review without accepting API-key values as action inputs.

### Changed

- The public Go module and source references now use `github.com/ComplyScan/ComplyScan`.
- CI now verifies the documented Go 1.22 minimum.
- Technical evidence now uses bounded keyword boundaries and local line context, preserves camel-case path signals, and applies the refined EU AI Act technical pack `0.1.2` terms derived from public-repository false-positive review.
- Reconciliation now consumes applicability conditions directly from the versioned technical pack instead of maintaining a second hard-coded Go mapping.
- Technical review now evaluates one candidate per model request and binds the returned decision to the objective and evidence fingerprint in trusted code instead of asking the model to reproduce identifiers.
- Technical review context includes a wider bounded window around the actual match before connected graph context, improving evidence available for mechanisms whose implementation spans nearby helpers.
- Transparent semantic guardrails retain executable grader and rubric implementations while rejecting discussion-only website, documentation, FAQ, and quiz components.
- Scan evidence bundles now use schema version 3 and include the AI component inventory, requirement-to-evidence reconciliation, isolated verification assurance, and advisory model observations in JSON, Markdown, and terminal output.
- Interactive setup now offers deterministic-only, local Ollama, or explicitly consented BYOK review and stores only the selected remote credential's environment-variable name.
- All review providers share the same bounded context, identifier binding, redaction, cache identity, and advisory-only result contract.

### Fixed

- Documentation values explicitly labelled as example, replacement, dummy, fake, or sample credentials no longer fail self-scans, while generic high-entropy assignments remain reportable.
- Malformed optional model search plans are safely skipped instead of aborting an otherwise valid bounded investigation.
- Negative technical conclusions require grounded context and cannot infer missing authorization or implementation evidence from an isolated excerpt.

### Testing

- Installer regressions cover unsupported systems, incomplete releases, and preservation of existing binaries after failed updates.
- Offline Ollama tests cover prompt injection, malformed output, invalid verdicts, duplicate bindings, and timeouts without downloading a model.
- Technical-evidence regression metrics cover candidate precision and recall, anchor and reachability accuracy, relationship recall, forbidden relationships, and language coverage.
- The pinned public-repository study records deterministic retrieval and live semantic precision, recall, specificity, coverage, token usage, and model disagreements separately.
- Regression tests cover the pinned study's medical-bias rubric false negative and interactive safety-quiz false positive; live validation explicitly bypasses cached observations.
- Three fresh `qwen3:8b` public-repository runs record identical effective decisions and worst-case raw-model versus post-guardrail precision, recall, specificity, coverage, duration, and variability without committing source or full rationales.

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
