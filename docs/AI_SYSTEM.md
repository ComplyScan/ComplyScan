# ComplyScan AI applicability assessment

| Field | Value |
| --- | --- |
| Product | ComplyScan |
| Version assessed | 0.1.7 |
| Assessment date | 2026-08-11 |
| Owner | ComplyScan maintainers |
| Status | Reviewed for v0.1.7 with model review retained as optional experimental advisory functionality; setup recovery contains configuration answers but no repository source or credential value, automatic compatibility is not quality validation, and the `qwen3.5:9b` technical-review gate remains outstanding; not legal advice |
| Next review | Required before promoting any model review from experimental status, then before any reassessment trigger below |

## Intended purpose

ComplyScan is an offline-capable developer CLI that identifies technical signals and missing repository evidence relevant to AI compliance engineering. It helps developers find items that require human review; it does not determine legal compliance, assign a legally binding risk classification, or replace qualified legal and compliance review. Model-free scanning remains available. Development setup recommends a small native BYOK cloud shortlist for advisory review and retains local Ollama and other compatible providers as explicit experimental paths for already-sanitised deterministic findings and bounded connected technical-objective context.

## Current system boundary

The current ComplyScan version consists of:

- bounded local repository discovery and file classification;
- optional bounded model-drafted setup suggestions that remain editable and require human confirmation;
- automatic source-free model compatibility qualification with a private expiring cache;
- private, source-free guided-setup recovery checkpoints with final human review before repository configuration is saved;
- typed dependency, import, endpoint, and environment signal extraction;
- deterministic, human-authored pattern and evidence rules backed by a labelled evaluation corpus;
- stable finding fingerprints, reasoned suppressions, and baselines;
- terminal, JSON, SARIF, and structured component-inventory reporting;
- reviewable AI-system and risk-assessment document generators;
- optional Git changed-file scope that preserves repository-wide governance checks;
- optional Ollama, native OpenAI/Anthropic/Gemini, and OpenAI-compatible hosted providers that generate normalized advisory observations for existing findings and technical-objective candidates;
- an optional container-only execution verifier that runs an explicitly supplied command against a read-only repository mount with networking disabled and attaches the bounded result to declared objective IDs;
- validated, version-controlled multi-system profiles with provisional EU AI Act scope screening and attributable human applicability decisions;
- validated, version-controlled repository path ownership with explicit shared, conflicting, unassigned, and single-system-inferred evidence states;
- explicit configured AI activities covering inference, training, fine-tuning, evaluation, automated decisions, agent tool use, synthetic-content generation, and unknown context;
- deterministic, versioned EU AI Act and NIST AI RMF technical packs with code-only objectives and no documentary or control-level compliance assessment;
- canonical technical controls that reuse one repository signal across source-specific objectives while preserving separate citations, applicability, and legal nature;
- deterministic reconciliation between screened EU technical requirements or voluntary NIST recommendations and independently discovered repository evidence, including visible gaps, mismatches, and unresolved system ownership;
- a language-neutral repository graph with a Go indexer for symbols, imports, calls, routes, configuration, tests, and conservative reachability;
- automatic local Markdown reports and versioned JSON evidence bundles; and
- a verified release installer and guided setup orchestrator that can provision a separately distributed Ollama runtime and user-selected model after explicit confirmation; and
- an optional GitHub Action that builds the CLI and can upload SARIF metadata to GitHub code scanning.

Interactive setup presents cloud review first, model-free analysis second, and experimental local Ollama third before repository context can reach a model; Ollama installation or model pulls require separate confirmation. Standard cloud setup contains only OpenAI, Anthropic, and Gemini with two exact quality-oriented candidates each. Selecting any hosted provider triggers a separate disclosure and default-no external-processing confirmation; automation requires `--allow-remote-review`. Every scan runs deterministic analysis, and the same command automatically adds the configured reviewer when one is enabled. Setup may use that reviewer after the privacy decision to prepare editable, repository-grounded answers; business, jurisdictional, operational, and legal facts still require the user. The standard shortlist is not yet a validated-model list: every cloud candidate is visibly marked as pending both maintained live gates. The provider adapters have fake-transport contract tests, and `scripts/validate-cloud-model.sh` can run the setup-draft and technical-review gates using an environment-only credential. No quality flag should change until the exact model and prompt versions pass and the dated result is reviewed. Ollama remains experimental; its historical `qwen3:8b` and `qwen3.5:9b` results retain their narrow attributed meaning. ComplyScan Cloud remains an inactive extension type.

The separate profile-draft prompt-version 4 gate passed its four-case `qwen3.5:9b` onboarding corpus on 2026-08-10 with 88.2% precision and recall, zero forbidden claims, and zero ungrounded references. It included a documentation-only hard negative and evaluated the final combined deterministic-plus-model draft. Two false positives and two false negatives remain documented in [profile-draft validation](profile-draft-validation.md), and every suggestion still requires human confirmation. This onboarding result does not satisfy or replace the pending `qwen3.5:9b` technical-review gate described above.

The technical packs, canonical control mapping, keyword-group evidence matcher, repository graph, activity-sensitive requirement mapping, ownership resolver, reconciliation, and retrieval execution remain deterministic in every configuration. The same canonical control can support separate EU and NIST objectives without conflating a legal-readiness result with a voluntary recommendation. With review disabled, no technical-objective context leaves the CLI process. Ollama receives the bounded context through loopback. A BYOK provider receives the same bounded context through its native, preset, or explicitly configured HTTPS endpoint after consent; profiles are not sent. For every declared system, trusted code creates a repository view containing only assigned or intentionally shared paths and rebuilds graph and retrieval context inside it. Candidate review follows an exact evidence fingerprint attributed to that system and framework objective; missing-evidence review searches only that system view. The model may propose one follow-up plan, but trusted deterministic code validates literal terms, searches only eligible allowed paths, and bounds returned excerpts. Cache and returned observations bind the system ID, objective, fingerprint, ownership scope, repository digest, and prompt/pack/provider identity. Model observations cannot change the deterministic reconciliation result. The pack's `technical-semantic-and-human` verification labels still require human review and never mean that a model established compliance.

## AI Act applicability assessment

Two operating configurations must now be distinguished.

In default deterministic mode, the working technical assessment remains that ComplyScan automatically executes human-defined rules and does not use model inference to generate findings. The earlier rationale for treating that configuration as traditional deterministic software therefore remains relevant.

In review-enabled mode, the selected model infers technical-evidence strengths, grounded supporting and contradictory claims, missing evidence, confidence, unresolved questions, and suggested actions from deterministic findings or bounded repository context. Trusted code validates cited paths, binds the result, applies semantic guardrails, and derives both assurance and an explicit repository verdict. “Implemented in reviewed code” requires strong, high-confidence cited support with neither contradictory evidence nor a missing objective element; all other combinations produce partial, not-implemented-in-reviewed-code, or cannot-determine outcomes. These are actionable code-review decisions, but they remain separate from legal applicability, deployment state, operational effectiveness, deterministic findings, and CI exit status. This inference-enabled configuration requires its own applicability assessment and maintained model-quality evaluation.

This assessment follows the distinction in Recital 12 and the European Commission's non-binding guidance between AI systems with an inference capability and simpler software that automatically executes human-defined rules. The project's free and open-source distribution may also be relevant to Article 2(12), but it is not the primary basis for this assessment and its exceptions must be considered if the product changes.

References:

- [Regulation (EU) 2024/1689](https://eur-lex.europa.eu/eli/reg/2024/1689/oj/eng)
- [European Commission guidance on the AI-system definition](https://digital-strategy.ec.europa.eu/en/library/commission-publishes-guidelines-ai-system-definition-facilitate-first-ai-acts-rules-application)

The assessment is not a conformity assessment or final legal determination. Applicable privacy, cybersecurity, consumer-protection, intellectual-property, contract, and other obligations remain separate from the AI Act classification.

## Data flows

The CLI loads its active configuration locally and excludes that exact file from repository discovery before inventory, rules, graph construction, or model context can be built. Normally this is `.complyscan.yml`; a custom file selected with `--config` receives the same treatment. Typed system facts are constructed separately from the loaded configuration for the analysis steps that require them. Other YAML files, including GitHub Actions workflows, retain their ordinary repository-evidence treatment. The CLI reads the remaining eligible repository files into process memory and evaluates them locally. It does not collect telemetry or upload source code in the default deterministic mode. Terminal and JSON findings may contain short, sanitised evidence excerpts. Technical-objective results store fingerprints, repository-relative paths, line numbers, file kinds, matched terms, symbol metadata, imports, relationships, reachability, and unresolved questions rather than source excerpts. Structured inventory evidence describes the detected technical signal rather than copying source lines. Secret-shaped evidence is redacted. Baseline files contain finding identity metadata but no source evidence. Completed scans retain immutable date-and-time-named Markdown and JSON pairs beneath `.complyscan/reports/history/` and refresh `latest.md` and `latest.json`; in-progress AI checkpoints update only the latest snapshots. Same-second scans receive a numeric suffix, while the scan ID remains inside each report. History is not automatically deleted. The complete report directory is excluded from scanning and added to `.gitignore` during initialization. Generated governance documents are written only to the user-selected local path and are protected from accidental overwrite by default.

When model review is explicitly enabled, ComplyScan first prepares targeted repository analysis from the discovered snapshot. Local inventory signals, technical-objective matches, production entry points, imports, callers, and bounded graph relationships select a compact package of redacted implementation excerpts, namespaced code objectives, typed declared system facts, and report-safe graph context. The model may request one plan containing at most three literal searches; trusted local code returns at most three new bounded excerpts and permits one final request. Files outside the package are not treated as absent. Explicit deep modes retain the previous broad full or hierarchical analysis. The model must cite submitted paths and valid line numbers; trusted code rejects unknown objectives, unknown systems, duplicate AI-use IDs, invented paths, out-of-range lines, invalid enums, and malformed output. The result is advisory and remains separate from deterministic reconciliation.

Bounded finding review also runs for visible, unsuppressed deterministic findings up to the configured maximum. The older per-objective technical flow runs only in `bounded-only` mode. Its candidate targets contain graph context and connected excerpts; missing-evidence targets contain deterministic search terms, coverage counts, ranked excerpts, and a bounded eligible-path manifest. Repository code and comments are labelled untrusted, source input is re-redacted, and raw source is not copied into reports. Ollama uses validated loopback without proxies. Native remote adapters use fixed official HTTPS endpoints; compatible presets use documented HTTPS bases; custom compatible providers require an explicit credential-free HTTPS base. Remote clients refuse redirects and may respect an operator-configured HTTPS proxy. Responses must conform to separate JSON schemas; returned text is re-redacted and length-limited.

Successful technical observations are cached outside the repository in the operating system's user-cache directory. Cache entries contain the bounded redacted observation and cryptographic digests, not the submitted source-context records. The model is instructed not to quote code, but its rationale may still describe sensitive repository details. Reuse requires an exact provider, model tag, prompt version, control-pack identity/digest, objective, evidence fingerprint, complete bounded-input digest, and full discovered-repository digest match. Entries are binding-validated, size-bounded, symlink-refusing, and atomically written with user-only file permissions. `--refresh-review` bypasses reads, and live validation always uses that flag. A model artifact changed in place without changing its configured tag is not automatically detectable; operators must refresh after such an update.

Optional execution verification invokes a user-selected, already installed Docker or Podman runtime and requires a preloaded image, exact executable/arguments, and declared objective IDs. `complyscan verify setup` locally inspects the configured system, every selected framework's conservative requirement or recommendation mapping, manifests, and test-file names; it names the source for each objective and saves only mappings explicitly selected and finally confirmed by the user. Detected repository scripts are used only to identify a native runner family, not copied into an executable command. The wizard cannot execute or provision anything. Reusable repository recipes remain inert unless a later scan explicitly passes `--verify`; one-off CLI flags are a separate explicit boundary. ComplyScan does not use a shell, pull an image, or enable container networking. It applies a read-only repository mount and root filesystem, drops capabilities, enables no-new-privileges, and bounds processes, memory, CPU, temporary storage, time, and captured output. The selected image and container runtime remain external trust boundaries. A pass adds separately labelled test-evidence assurance to the declared objective and system without changing deterministic evidence or mapping. Bounded redacted result metadata and output may enter configured model context only when both the declared objective and system match that investigation target. Test output may itself contain repository or runtime details. A result cannot prove objective coverage, establish production effectiveness, or change compliance, legal applicability, findings, or failure-threshold status.

Default setup first inspects the repository locally without a model, then asks for the analysis boundary. After that boundary is established and the selected model passes compatibility checking, setup may send bounded, redacted repository context to prepare editable suggestions. The user confirms system facts, selects technical mappings, and answers only their relevant follow-up questions; selecting a technical pack is not itself a legal applicability decision. Advanced setup remains available for multiple systems, full profiles, evidence ownership, and attributed human applicability decisions. After each completed major stage, terminal setup may save the configuration in a repository-keyed recovery draft under the operating system's user-cache directory. The draft contains no repository source or API-key value, is size-bounded, atomically written with user-only permissions, refuses symlinks, expires after 30 days, and is deleted after `.complyscan.yml` is saved. The final review presents two outcomes: save and run ComplyScan, or save without scanning. The global left-arrow control revisits earlier settings, while Ctrl+C cancels setup. After separate explicit confirmations, setup may invoke Homebrew, download and execute Ollama's official Linux installer, start the Homebrew Ollama service, or invoke `ollama pull` for the selected model. Profiles and repository source are not passed to those provisioning commands. Before interactive setup or a scan trusts an unseen model with repository context, ComplyScan sends one source-free synthetic finding and requires a correctly bound schema-valid response. Successful compatibility results are cached outside the repository for 30 days against provider, exact model, available Ollama digest, and every prompt-contract version; failures are not cached. Non-interactive setup makes this request only with `--qualify-model`. The request may incur remote-provider cost but contains no repository data. Compatibility demonstrates only minimum protocol behavior, not review quality or benchmark validation. Remote setup saves only provider, model, limits, and an environment-variable name after disclosure and consent. The credential value remains in the shell or CI secret store. `doctor --probe-review` refreshes the same synthetic compatibility result and may incur cost.

When explicitly enabled, the GitHub Action uploads SARIF containing finding messages, repository-relative paths, line numbers, remediation text, and fingerprints. ComplyScan SARIF intentionally omits source excerpts and detected credentials. When any reviewer is enabled, SARIF additionally carries advisory provider/model metadata, verdicts, confidence, rationales, and suggested actions. A BYOK credential must enter the job through a secret-backed environment variable, never an action input.

## Human oversight and expected use

Every finding is a review prompt. Users are expected to:

- inspect the complete system and deployment context;
- confirm or reject technical signals;
- treat every model observation as untrusted advisory input rather than a finding or legal conclusion;
- complete and approve generated governance scaffolds rather than treating them as finished assessments;
- document suppression decisions with reasons;
- avoid treating a clear scan as proof of compliance; and
- obtain qualified review for legal classifications and obligations.

Guided setup records declared facts and optional human decisions; it does not verify the truth or legal sufficiency of either. The repository profile must therefore preserve `unknown` values where evidence is missing, identify the human reviewer when confirmed, and be reassessed when the system's purpose, markets, actors, users, affected groups, data, deployment, or decision impact changes.

## Reassessment triggers

This assessment must be reviewed before any model review is promoted from experimental status and again if ComplyScan adds any of the following:

- another remote model-backed provider, a configurable remote destination, or a material provider API change;
- learned, adaptive, probabilistic, or model-derived logic that creates, removes, re-severities, suppresses, or gates deterministic findings;
- remote, over-budget, mis-scoped, or non-consensual source-file or repository-content disclosure to a review provider;
- hosted scanning, complete or persistent source-code upload, telemetry, or persistent scan storage;
- automated legal conclusions or risk classifications presented without meaningful human review;
- decisions or recommendations used in employment, education, credit, essential services, law enforcement, migration, biometrics, safety components, or other regulated contexts; or
- a material change to intended purpose, users, affected persons, distribution, or commercial model.

The review must record the system definition analysis, operator roles, risk classification, data flows, transparency duties, human oversight, testing evidence, and any required conformity or registration steps.
