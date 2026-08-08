# ComplyScan AI applicability assessment

| Field | Value |
| --- | --- |
| Product | ComplyScan |
| Version assessed | 0.1.4 development |
| Assessment date | 2026-08-07 |
| Owner | ComplyScan maintainers |
| Status | Reassessment in progress following graph-backed technical context and optional local or BYOK model review; not legal advice |
| Next review | Required before promoting any model review from experimental status, then before any reassessment trigger below |

## Intended purpose

ComplyScan is an offline-by-default developer CLI that identifies technical signals and missing repository evidence relevant to AI compliance engineering. It helps developers find items that require human review; it does not determine legal compliance, assign a legally binding risk classification, or replace qualified legal and compliance review. Users may explicitly enable an advisory review layer through local Ollama or a BYOK OpenAI, Anthropic, or Gemini account for already-sanitised deterministic findings and bounded connected technical-objective context.

## Current system boundary

The current ComplyScan development version consists of:

- bounded local repository discovery and file classification;
- typed dependency, import, endpoint, and environment signal extraction;
- deterministic, human-authored pattern and evidence rules backed by a labelled evaluation corpus;
- stable finding fingerprints, reasoned suppressions, and baselines;
- terminal, JSON, SARIF, and structured component-inventory reporting;
- reviewable AI-system and risk-assessment document generators;
- optional Git changed-file scope that preserves repository-wide governance checks;
- optional Ollama, OpenAI, Anthropic, and Gemini providers that generate normalized advisory observations for existing findings and technical-objective candidates;
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

Model review is disabled by the built-in scan default. Interactive setup presents Ollama first and requires separate confirmation for installation or model pull. Selecting OpenAI, Anthropic, or Gemini triggers a separate disclosure and default-no external-processing confirmation; automation requires `--allow-remote-review`. The default scan remains deterministic and makes no model call. Ollama's tested experimental configuration selects `qwen3:8b`. A prompt-version-8 two-target smoke run passed on 2026-08-06 after adding the isolated-test interpretation boundary. The prompt-version 10 multi-framework fixture gate passed on 2026-08-07 across Go, Python, and TypeScript with separate EU and NIST objective bindings, no semantic corrections, and bounded prompt-injection content. Its TypeScript wall-clock measurement is not usable because the host laptop slept during the run, so the gate remains a quality-contract result rather than a performance claim. BYOK adapters have fake-transport contract tests but no maintained live-model quality baseline yet; they remain experimental. ComplyScan Cloud remains an inactive extension type.

The technical packs, canonical control mapping, keyword-group evidence matcher, repository graph, activity-sensitive requirement mapping, ownership resolver, reconciliation, and retrieval execution remain deterministic in every configuration. The same canonical control can support separate EU and NIST objectives without conflating a legal-readiness result with a voluntary recommendation. With review disabled, no technical-objective context leaves the CLI process. Ollama receives the bounded context through loopback. A BYOK provider receives the same bounded context through its fixed official HTTPS endpoint after consent; profiles are not sent. For every declared system, trusted code creates a repository view containing only assigned or intentionally shared paths and rebuilds graph and retrieval context inside it. Candidate review follows an exact evidence fingerprint attributed to that system and framework objective; missing-evidence review searches only that system view. The model may propose one follow-up plan, but trusted deterministic code validates literal terms, searches only eligible allowed paths, and bounds returned excerpts. Cache and returned observations bind the system ID, objective, fingerprint, ownership scope, repository digest, and prompt/pack/provider identity. Model observations cannot change the deterministic reconciliation result. The pack's `technical-semantic-and-human` verification labels still require human review and never mean that a model established compliance.

## AI Act applicability assessment

Two operating configurations must now be distinguished.

In default deterministic mode, the working technical assessment remains that ComplyScan automatically executes human-defined rules and does not use model inference to generate findings. The earlier rationale for treating that configuration as traditional deterministic software therefore remains relevant.

In review-enabled mode, the selected model infers advisory verdicts or technical-evidence strengths, grounded supporting and contradictory claims, missing evidence, conclusions, confidence, unresolved questions, and suggested review actions from deterministic findings or bounded repository context. Trusted code validates cited paths, binds the result, applies semantic guardrails, and derives the assurance level. This inference-enabled configuration requires its own applicability assessment before promotion from experimental status. Keeping model observations advisory and separate from deterministic evidence is a control, not by itself an exemption or classification decision.

This assessment follows the distinction in Recital 12 and the European Commission's non-binding guidance between AI systems with an inference capability and simpler software that automatically executes human-defined rules. The project's free and open-source distribution may also be relevant to Article 2(12), but it is not the primary basis for this assessment and its exceptions must be considered if the product changes.

References:

- [Regulation (EU) 2024/1689](https://eur-lex.europa.eu/eli/reg/2024/1689/oj/eng)
- [European Commission guidance on the AI-system definition](https://digital-strategy.ec.europa.eu/en/library/commission-publishes-guidelines-ai-system-definition-facilitate-first-ai-acts-rules-application)

The assessment is not a conformity assessment or final legal determination. Applicable privacy, cybersecurity, consumer-protection, intellectual-property, contract, and other obligations remain separate from the AI Act classification.

## Data flows

The CLI reads eligible repository files into process memory and evaluates them locally. It does not collect telemetry or upload source code in the default deterministic mode. Terminal and JSON findings may contain short, sanitised evidence excerpts. Technical-objective results store fingerprints, repository-relative paths, line numbers, file kinds, matched terms, symbol metadata, imports, relationships, reachability, and unresolved questions rather than source excerpts. Structured inventory evidence describes the detected technical signal rather than copying source lines. Secret-shaped evidence is redacted. Baseline files contain finding identity metadata but no source evidence. Every successful scan atomically writes `.complyscan/reports/latest.md` and `latest.json`; the directory is excluded from scanning and added to `.gitignore` during initialization. Generated governance documents are written only to the user-selected local path and are protected from accidental overwrite by default.

When model review is explicitly enabled, ComplyScan first selects visible and unsuppressed deterministic findings up to the configured maximum. It sends bounded, re-redacted records containing the fingerprint, rule ID, title, severity, category, message, relative path, line, short evidence, remediation, and confidence. A separate technical flow selects existing candidates and likely-required objectives without a candidate, up to the same maximum. Candidate targets contain graph context and connected excerpts. Missing-evidence targets contain deterministic search terms, eligible/matching-file coverage, up to six ranked excerpts, and at most 200 eligible paths. One planning call may request up to three literal queries and path substrings; trusted code returns at most three 2,000-character excerpts from eligible discovered files before the final decision. The evidence fingerprint is withheld from the model; trusted code attaches the sole returned decision to the submitted objective/fingerprint pair and rejects any evidence claim citing an unsubmitted path. It does not send a complete repository or unbounded file. Repository code and comments are labelled untrusted, source input is re-redacted, and raw excerpts are not copied into reports. Ollama uses validated loopback without proxies. Remote adapters use fixed official HTTPS endpoints, refuse redirects, may respect an operator-configured HTTPS proxy, and request non-persistent processing where the provider API exposes a request-level storage switch. Responses must conform to separate JSON schemas; returned text is re-redacted and length-limited.

Successful technical observations are cached outside the repository in the operating system's user-cache directory. Cache entries contain the bounded redacted observation and cryptographic digests, not the submitted source-context records. The model is instructed not to quote code, but its rationale may still describe sensitive repository details. Reuse requires an exact provider, model tag, prompt version, control-pack identity/digest, objective, evidence fingerprint, complete bounded-input digest, and full discovered-repository digest match. Entries are binding-validated, size-bounded, symlink-refusing, and atomically written with user-only file permissions. `--refresh-review` bypasses reads, and live validation always uses that flag. A model artifact changed in place without changing its configured tag is not automatically detectable; operators must refresh after such an update.

Optional execution verification invokes a user-selected, already installed Docker or Podman runtime and requires a preloaded image, exact executable/arguments, and declared objective IDs. `complyscan verify setup` locally inspects the configured system, every selected framework's conservative requirement or recommendation mapping, manifests, and test-file names; it names the source for each objective and saves only mappings explicitly selected and finally confirmed by the user. Detected repository scripts are used only to identify a native runner family, not copied into an executable command. The wizard cannot execute or provision anything. Reusable repository recipes remain inert unless a later scan explicitly passes `--verify`; one-off CLI flags are a separate explicit boundary. ComplyScan does not use a shell, pull an image, or enable container networking. It applies a read-only repository mount and root filesystem, drops capabilities, enables no-new-privileges, and bounds processes, memory, CPU, temporary storage, time, and captured output. The selected image and container runtime remain external trust boundaries. A pass adds separately labelled test-evidence assurance to the declared objective and system without changing deterministic evidence or mapping. Bounded redacted result metadata and output may enter configured model context only when both the declared objective and system match that investigation target. Test output may itself contain repository or runtime details. A result cannot prove objective coverage, establish production effectiveness, or change compliance, legal applicability, findings, or failure-threshold status.

The setup questionnaire is local. After separate explicit confirmations, setup may invoke Homebrew, download and execute Ollama's official Linux installer, start the Homebrew Ollama service, or invoke `ollama pull` for the selected model. Profiles and repository source are not passed to those commands. Remote setup makes no provider request; it saves only provider, model, limits, and an environment-variable name after disclosure and consent. The credential value remains in the shell or CI secret store. `doctor --probe-review` is the separate explicit boundary for a small live synthetic request, which may incur cost.

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
- remote, unbounded, or non-consensual source-file or repository-content disclosure to a review provider;
- hosted scanning, complete or persistent source-code upload, telemetry, or persistent scan storage;
- automated legal conclusions or risk classifications presented without meaningful human review;
- decisions or recommendations used in employment, education, credit, essential services, law enforcement, migration, biometrics, safety components, or other regulated contexts; or
- a material change to intended purpose, users, affected persons, distribution, or commercial model.

The review must record the system definition analysis, operator roles, risk classification, data flows, transparency duties, human oversight, testing evidence, and any required conformity or registration steps.
