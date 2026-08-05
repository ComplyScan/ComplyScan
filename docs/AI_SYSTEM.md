# ComplyScan AI applicability assessment

| Field | Value |
| --- | --- |
| Product | ComplyScan |
| Version assessed | 0.1.2 |
| Assessment date | 2026-08-03 |
| Owner | ComplyScan maintainers |
| Status | Reassessment in progress following graph-backed technical context and optional Ollama review; not legal advice |
| Next review | Required before promoting Ollama review from experimental status, then before any reassessment trigger below |

## Intended purpose

ComplyScan is an offline-by-default developer CLI that identifies technical signals and missing repository evidence relevant to AI compliance engineering. It helps developers find items that require human review; it does not determine legal compliance, assign a legally binding risk classification, or replace qualified legal and compliance review. Users may explicitly enable an advisory Ollama review layer for already-sanitised deterministic findings and bounded connected technical-objective context.

## Current system boundary

ComplyScan 0.1.2 consists of:

- bounded local repository discovery and file classification;
- typed dependency, import, endpoint, and environment signal extraction;
- deterministic, human-authored pattern and evidence rules backed by a labelled evaluation corpus;
- stable finding fingerprints, reasoned suppressions, and baselines;
- terminal, JSON, SARIF, and structured component-inventory reporting;
- reviewable AI-system and risk-assessment document generators;
- optional Git changed-file scope that preserves repository-wide governance checks;
- an optional Ollama provider that generates separate advisory observations for existing findings and technical-objective candidates through a loopback-only local API;
- validated, version-controlled multi-system profiles with provisional EU AI Act scope screening and attributable human applicability decisions;
- a deterministic, versioned EU AI Act technical pack with code-only objectives and no documentary or control-level compliance assessment;
- a language-neutral repository graph with a Go indexer for symbols, imports, calls, routes, configuration, tests, and conservative reachability;
- automatic local Markdown reports and versioned JSON evidence bundles; and
- a verified release installer and guided setup orchestrator that can provision a separately distributed Ollama runtime and user-selected model after explicit confirmation; and
- an optional GitHub Action that builds the CLI and can upload SARIF metadata to GitHub code scanning.

Ollama review is disabled by the built-in scan default. Interactive setup recommends it, but the user must confirm the selection and separately confirm any runtime installation or model pull. The default scan remains deterministic and makes no model call. When explicitly enabled, ComplyScan calls a configured model through Ollama's loopback API. The experimental configuration selects `qwen3:8b`, and a live quality/resource gate remains open before it is promoted to stable status. OpenAI, Anthropic, Gemini, and ComplyScan Cloud remain inactive extension types.

The technical pack, keyword-group evidence matcher, repository graph, and context selection remain deterministic in both configurations. With Ollama disabled, no technical-objective context leaves the CLI process. With Ollama enabled, existing technical candidates are reviewed in a second request using bounded graph metadata and connected excerpts; profiles are not sent. The pack's `technical-semantic-and-human` verification labels still require human review and never mean that a model established compliance. Generated Markdown and JSON reports remain local unless a future dashboard connection is explicitly enabled.

## AI Act applicability assessment

Two operating configurations must now be distinguished.

In default deterministic mode, the working technical assessment remains that ComplyScan automatically executes human-defined rules and does not use model inference to generate findings. The earlier rationale for treating that configuration as traditional deterministic software therefore remains relevant.

In Ollama-enabled mode, a model infers advisory verdicts or technical-evidence strengths, rationales, confidence, unresolved questions, and suggested review actions from deterministic findings or bounded connected code context. The earlier project-wide conclusion cannot be extended to this configuration without further analysis. Before Ollama review is promoted from experimental status, maintainers must record a qualified assessment of whether the configured system meets the Article 3(1) definition, the relevant value-chain roles, the effect of free and open-source distribution, and any resulting obligations. Keeping model observations advisory and separate from deterministic evidence is a control, not by itself an exemption or classification decision.

This assessment follows the distinction in Recital 12 and the European Commission's non-binding guidance between AI systems with an inference capability and simpler software that automatically executes human-defined rules. The project's free and open-source distribution may also be relevant to Article 2(12), but it is not the primary basis for this assessment and its exceptions must be considered if the product changes.

References:

- [Regulation (EU) 2024/1689](https://eur-lex.europa.eu/eli/reg/2024/1689/oj/eng)
- [European Commission guidance on the AI-system definition](https://digital-strategy.ec.europa.eu/en/library/commission-publishes-guidelines-ai-system-definition-facilitate-first-ai-acts-rules-application)

The assessment is not a conformity assessment or final legal determination. Applicable privacy, cybersecurity, consumer-protection, intellectual-property, contract, and other obligations remain separate from the AI Act classification.

## Data flows

The CLI reads eligible repository files into process memory and evaluates them locally. It does not collect telemetry or upload source code in the default deterministic mode. Terminal and JSON findings may contain short, sanitised evidence excerpts. Technical-objective results store fingerprints, repository-relative paths, line numbers, file kinds, matched terms, symbol metadata, imports, relationships, reachability, and unresolved questions rather than source excerpts. Structured inventory evidence describes the detected technical signal rather than copying source lines. Secret-shaped evidence is redacted. Baseline files contain finding identity metadata but no source evidence. Every successful scan atomically writes `.complyscan/reports/latest.md` and `latest.json`; the directory is excluded from scanning and added to `.gitignore` during initialization. Generated governance documents are written only to the user-selected local path and are protected from accidental overwrite by default.

When Ollama review is explicitly enabled, ComplyScan first selects visible and unsuppressed deterministic findings up to the configured maximum. It sends bounded, re-redacted records containing the fingerprint, rule ID, title, severity, category, message, relative path, line, short evidence, remediation, and confidence. A separate technical flow selects existing candidates up to the same maximum and sends one candidate per request with its objective ID, reachability, imports, relationships, unresolved questions, a bounded matched-code window, and no more than six bounded connected symbol excerpts. The evidence fingerprint is withheld from the model; trusted code attaches the sole returned decision to the submitted objective/fingerprint pair. It does not send a complete repository or unbounded file. Repository code and comments are labelled untrusted, source input is re-redacted, and raw excerpts are not copied into reports. Requests go directly to a validated loopback endpoint without HTTP proxies or redirects. Responses must conform to separate JSON schemas; returned text is re-redacted and length-limited.

Successful technical observations are cached outside the repository in the operating system's user-cache directory. Cache entries contain the bounded redacted observation and cryptographic digests, not the submitted source-context records. The model is instructed not to quote code, but its rationale may still describe sensitive repository details. Reuse requires an exact provider, model tag, prompt version, control-pack identity/digest, objective, evidence fingerprint, and complete bounded-input digest match. Entries are binding-validated, size-bounded, symlink-refusing, and atomically written with user-only file permissions. `--refresh-review` bypasses reads, and live validation always uses that flag. A model artifact changed in place without changing its configured tag is not automatically detectable; operators must refresh after such an update.

The setup questionnaire is local. After separate explicit confirmations, setup may invoke Homebrew, download and execute Ollama's official Linux installer, start the Homebrew Ollama service, or invoke `ollama pull` for the selected model. Profiles and repository source are not passed to those commands. Non-interactive setup performs no installation or pull unless the operator supplies the corresponding flag. Ollama's installers, model registry, model downloads, and any user-selected cloud-model routing are external processing and supply-chain boundaries that must be assessed by the operator. If Ollama review is used in CI, the service runs in that job environment rather than on a developer workstation.

When explicitly enabled, the GitHub Action uploads SARIF containing finding messages, repository-relative paths, line numbers, remediation text, and fingerprints. ComplyScan SARIF intentionally omits source excerpts and detected credentials. When Ollama is enabled, SARIF additionally carries advisory provider/model metadata, verdicts, confidence, rationales, and suggested actions. GitHub and the repository owner govern that separate processing environment.

## Human oversight and expected use

Every finding is a review prompt. Users are expected to:

- inspect the complete system and deployment context;
- confirm or reject technical signals;
- treat Ollama observations as untrusted advisory input rather than findings or legal conclusions;
- complete and approve generated governance scaffolds rather than treating them as finished assessments;
- document suppression decisions with reasons;
- avoid treating a clear scan as proof of compliance; and
- obtain qualified review for legal classifications and obligations.

Guided setup records declared facts and optional human decisions; it does not verify the truth or legal sufficiency of either. The repository profile must therefore preserve `unknown` values where evidence is missing, identify the human reviewer when confirmed, and be reassessed when the system's purpose, markets, actors, users, affected groups, data, deployment, or decision impact changes.

## Reassessment triggers

This assessment must be reviewed before Ollama review is promoted from experimental status and again if ComplyScan adds any of the following:

- any remote model-backed provider or permission for non-loopback review endpoints;
- learned, adaptive, probabilistic, or model-derived logic that creates, removes, re-severities, suppresses, or gates deterministic findings;
- remote, unbounded, or non-consensual source-file or repository-content disclosure to a review provider;
- hosted scanning, source-code upload, telemetry, or persistent scan storage;
- automated legal conclusions or risk classifications presented without meaningful human review;
- decisions or recommendations used in employment, education, credit, essential services, law enforcement, migration, biometrics, safety components, or other regulated contexts; or
- a material change to intended purpose, users, affected persons, distribution, or commercial model.

The review must record the system definition analysis, operator roles, risk classification, data flows, transparency duties, human oversight, testing evidence, and any required conformity or registration steps.
