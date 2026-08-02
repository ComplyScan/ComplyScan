# ComplyScan AI applicability assessment

| Field | Value |
| --- | --- |
| Product | ComplyScan |
| Version assessed | 0.2.0 |
| Assessment date | 2026-08-02 |
| Owner | ComplyScan maintainers |
| Status | Maintained technical assessment; not legal advice |
| Next review | Before any reassessment trigger below, or by 2027-02-02 |

## Intended purpose

ComplyScan is an offline developer CLI that identifies technical signals and missing repository evidence relevant to AI compliance engineering. It helps developers find items that require human review; it does not determine legal compliance, assign a legally binding risk classification, or replace qualified legal and compliance review.

## Current system boundary

Version 0.2.0 consists of:

- bounded local repository discovery and file classification;
- deterministic, human-authored pattern and evidence rules;
- stable finding fingerprints, reasoned suppressions, and baselines;
- terminal, JSON, and SARIF reporting; and
- an optional GitHub Action that builds the CLI and can upload SARIF metadata to GitHub code scanning.

The provider names and interfaces in the source tree are reserved extension points and detection signatures. Version 0.2.0 does not instantiate an AI provider, call a model, train or adapt a model, or make a network request from the CLI.

## AI Act applicability assessment

The working technical assessment is that ComplyScan 0.2.0 is traditional deterministic software rather than an AI system under Article 3(1) of Regulation (EU) 2024/1689. Its outputs are produced by rules defined by natural persons; it does not derive a model or algorithm from input data or use model inference to decide how to generate findings.

This assessment follows the distinction in Recital 12 and the European Commission's non-binding guidance between AI systems with an inference capability and simpler software that automatically executes human-defined rules. The project's free and open-source distribution may also be relevant to Article 2(12), but it is not the primary basis for this assessment and its exceptions must be considered if the product changes.

References:

- [Regulation (EU) 2024/1689](https://eur-lex.europa.eu/eli/reg/2024/1689/oj/eng)
- [European Commission guidance on the AI-system definition](https://digital-strategy.ec.europa.eu/en/library/commission-publishes-guidelines-ai-system-definition-facilitate-first-ai-acts-rules-application)

The assessment is not a conformity assessment or final legal determination. Applicable privacy, cybersecurity, consumer-protection, intellectual-property, contract, and other obligations remain separate from the AI Act classification.

## Data flows

The CLI reads eligible repository files into process memory and evaluates them locally. It does not collect telemetry or upload source code. Terminal and JSON reports may contain short, sanitised evidence excerpts. Secret-shaped evidence is redacted. Baseline files contain finding identity metadata but no source evidence.

When explicitly enabled, the GitHub Action uploads SARIF containing finding messages, repository-relative paths, line numbers, remediation text, and fingerprints. ComplyScan SARIF intentionally omits source excerpts and detected credentials. GitHub and the repository owner govern that separate processing environment.

## Human oversight and expected use

Every finding is a review prompt. Users are expected to:

- inspect the complete system and deployment context;
- confirm or reject technical signals;
- document suppression decisions with reasons;
- avoid treating a clear scan as proof of compliance; and
- obtain qualified review for legal classifications and obligations.

## Reassessment triggers

This assessment must be reviewed before release if ComplyScan adds any of the following:

- local or remote model inference, including Ollama;
- an OpenAI, Anthropic, Gemini, or other model-backed review provider;
- learned, adaptive, probabilistic, or model-derived detection logic;
- hosted scanning, source-code upload, telemetry, or persistent scan storage;
- automated legal conclusions or risk classifications presented without meaningful human review;
- decisions or recommendations used in employment, education, credit, essential services, law enforcement, migration, biometrics, safety components, or other regulated contexts; or
- a material change to intended purpose, users, affected persons, distribution, or commercial model.

The review must record the system definition analysis, operator roles, risk classification, data flows, transparency duties, human oversight, testing evidence, and any required conformity or registration steps.
