# ComplyScan

> ComplyScan is a developer-first scanner that identifies potential AI compliance risks and missing governance evidence before code reaches production.

ComplyScan is an open-source, offline-by-default CLI for finding technical signals that deserve review during EU AI Act readiness work. Guided setup records factual system context and attributable human applicability decisions; a versioned technical pack maps code and configuration signals to EU AI Act technical objectives; and scans inventory likely AI providers and frameworks, look for risky logging and hardcoded credentials, and check whether repository-level AI-system and risk-classification evidence is present. An explicitly enabled Ollama layer can add local advisory review of both deterministic findings and bounded technical-objective context without changing either evidence layer.

ComplyScan does **not** interpret a complete system, determine an EU AI Act classification, certify compliance, or replace legal and compliance professionals. A finding is a review prompt—not a claim that a system violates the law.

## Install

> Current release: v0.1.2. Ollama review is optional and experimental; deterministic scanning remains the default.

Starting with v0.1.2, macOS and Linux users can install ComplyScan and immediately start guided setup with one command:

```bash
curl -fsSL https://complyscan.github.io/ComplyScan/install.sh | sh
```

The installer is published from this repository through GitHub Pages. It detects the operating system and CPU architecture, downloads the matching release archive, verifies its published SHA-256 checksum, installs `complyscan` into `~/.local/bin`, and launches `complyscan setup` through the terminal. It does not use `sudo`. Pass `--no-setup` for automation or pin both the installer and binary version:

```bash
curl -fsSL https://github.com/ComplyScan/ComplyScan/releases/download/v0.1.2/install.sh | sh -s -- --version v0.1.2 --no-setup
```

Prebuilt archives for macOS, Linux, and Windows are also available on [GitHub Releases](https://github.com/ComplyScan/ComplyScan/releases). The current stable release can be installed with Go 1.22 or newer:

```bash
go install github.com/ComplyScan/ComplyScan/cmd/complyscan@latest
```

Building the development version from source requires Go 1.22 or newer:

```bash
git clone https://github.com/ComplyScan/ComplyScan.git
cd complyscan
go build -o complyscan ./cmd/complyscan
```

Release builds can inject metadata with `-ldflags`:

```bash
go build -ldflags "-X main.version=0.1.2-dev -X main.commit=$(git rev-parse --short HEAD) -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o complyscan ./cmd/complyscan
```

## Quick start

```bash
complyscan setup
complyscan init
complyscan profile show
complyscan profile setup # add context to an existing configuration
complyscan framework list
complyscan framework assess .
complyscan framework assess . --format json
complyscan scan .
complyscan scan . --format json
complyscan scan . --format sarif > complyscan.sarif
complyscan scan . --severity high --no-color
complyscan scan . --tracked-only
complyscan scan . --exclude fixtures --max-files 10000
complyscan scan . --changed-since main
complyscan scan . --review ollama --ollama-model qwen3:8b
complyscan scan . --report-dir .complyscan/reports
complyscan scan . --no-report
complyscan inventory .
complyscan inventory . --format json
complyscan generate ai-system .
complyscan generate risk-assessment .
complyscan baseline .
complyscan doctor .
complyscan version
```

`setup` is the recommended first command. It creates or updates `.complyscan.yml`, runs the factual system and human-applicability questionnaire, recommends local Ollama review, lets the user choose any Ollama model, offers to install and start Ollama when it is missing, offers to pull the selected model, and can run the first scan. Each software installation and model download requires a separate confirmation. If setup cannot finish an external installation or download, it still saves the collected repository configuration and prints the exact recovery command.

`doctor` performs an offline readiness check for the installed build, repository configuration, Git detection, report-directory permissions, the Ollama executable, its loopback service, and the configured model. Missing optional tools are warnings; a required but unavailable Ollama service or model is a blocking failure.

Maintainers can run the repeatable live-model quality and resource gate with `./scripts/validate-ollama.sh` after `qwen3:8b` is available. See [Ollama live-model validation](docs/ollama-validation.md) for the enforced production/test-only expectations and saved artifacts.

Interactive setup selects `qwen3:8b` by default, but the user can enter another local model or decline Ollama and keep deterministic scanning. Automation never installs software or downloads a model unless `--install-ollama` or `--pull-model` is explicitly passed. Use `--non-interactive --review none` for a network-free starter configuration, or `--skip-ollama-install`, `--skip-model-pull`, and `--skip-scan` to control individual interactive steps.

`scan` defaults to the current directory, so `complyscan scan` and `complyscan scan .` are equivalent. In a terminal, `init` guides you through factual questions about each system's purpose, operating regions, value-chain role, use-case domain, users, affected groups, decision impact, data, human oversight, and deployment. Use `unknown` rather than guessing. Redirected or CI initialization is non-interactive and can be made explicit with `--non-interactive`.

`profile show` reports conservative EU AI Act scope and high-risk screening from those declared facts. `profile setup` adds a system to an existing config; use `--replace` to update a profile with the same ID. Automated screening, human decisions, and missing context remain separate. ComplyScan never converts a scope signal into a compliance certificate.

`inventory` produces a component-focused view rather than compliance findings. It aggregates detected providers and frameworks with their technical evidence, runtime/test/configuration scope, package versions, confidence, and source locations. Its JSON output has a versioned schema for downstream tooling.

The two `generate` commands use that inventory to create `docs/AI_SYSTEM.md` and `docs/risk-assessment.md`. The generated files are deliberately marked as drafts, preserve discovery warnings, and contain explicit human-review fields. Existing documents are protected unless `--force` is supplied.

Terminal scans print findings as rules discover them and finish with applicability context, code-only technical evidence, and the final summary. Every successful scan also atomically writes `.complyscan/reports/latest.md` for people and `.complyscan/reports/latest.json` as the versioned machine-readable evidence bundle. `complyscan init` adds that generated directory to `.gitignore`; scans always exclude it from subsequent discovery. Use `--no-report` to disable persistence or `--report-dir` to select another directory inside the target.

JSON and SARIF 2.1.0 output remain buffered so they are valid and deterministically ordered. JSON stdout and `latest.json` include the technical evidence bundle. SARIF includes source locations and stable partial fingerprints for code-scanning integrations, but omits technical-objective summaries that have no single code-scanning location.

Example terminal report:

```text
ComplyScan found 3 potential issues

HIGH   AI-LOG-001  Prompt or model response may be logged
       internal/chat/service.go:42
       A value named userPrompt appears to be passed to a logging function.

MED    AI-DOC-001  AI-system documentation not found
       AI-related technical usage was detected, but no model card or
       AI-system documentation was found in the repository.

MED    AI-RISK-001  AI risk classification not found
       No repository-level AI risk-classification evidence was found.

Summary: 1 high, 2 medium

Reports saved:
  Human-readable: .complyscan/reports/latest.md
  Evidence bundle: .complyscan/reports/latest.json
```

## Rules

| Rule | Severity | Purpose |
| --- | --- | --- |
| `AI-DISC-001` | Info | Inventory technically evidenced AI providers and frameworks, including OpenAI, Anthropic, Gemini, Mistral, Cohere, Hugging Face, Ollama, LiteLLM, LangChain, LlamaIndex, Vercel AI SDK, and OpenRouter. Results are aggregated by component with representative locations. |
| `AI-LOG-001` | High | Flag conservative cases where prompt-like or response-like values appear in common Go, Python, JavaScript, or TypeScript logging calls. |
| `AI-SEC-001` | High | Detect likely hardcoded AI API credentials and report only redacted evidence. |
| `AI-DOC-001` | Medium | When AI usage is detected, check for a model card or AI-system documentation. |
| `AI-RISK-001` | Medium | When AI usage is detected, check for repository-level AI risk-classification evidence. |

Provider detection requires typed evidence: a recognised dependency declaration, source import, service endpoint, or environment-variable access. Plain provider names and documentation prose are not treated as runtime usage. The maintained labelled corpus measures precision and recall across positive and hard-negative examples.

The deterministic rule layer remains deliberately technical. Technical-objective matching is a separate evidence layer: neither a rule finding nor a keyword match is treated as a legal conclusion.

## Versioned technical evidence

The built-in `eu-ai-act-technical-evidence` pack version `0.1.0` contains 13 code objectives associated with Articles 9, 10, 12, 14, 15, and 50 of Regulation (EU) 2024/1689. They cover technical signals for risk-control tests, dataset validation, bias evaluation, automatic logging, retention configuration, human review, override and stop mechanisms, performance thresholds, robustness, AI-specific security, interaction disclosure, and synthetic-content marking.

The pack accepts only source, configuration, dependency, test, CI, container, and infrastructure file kinds. It does not contain objectives for policies, risk assessments, training records, conformity documents, attestations, or other evidence intended for a future dashboard. The CLI runs technical checks even when no system profile exists; declared context remains a separate, provisional report section rather than an activation gate.

Every technical objective receives one of these statuses:

| Status | Meaning |
| --- | --- |
| `candidate-evidence` | One or more eligible files matched all configured signal groups. The code still requires technical and human verification. |
| `not-detected` | The bounded scan did not locate the configured technical signal. This does not prove the implementation is absent. |
| `not-evaluated` | The technical check could not be evaluated. This status is reserved for explicit limitations rather than treated as a failure. |

Evidence matching requires every configured keyword group and path signal to match one eligible file. A language-neutral repository graph then attaches bounded structural context to each candidate. The current indexers support Go, Python, JavaScript, and TypeScript and record functions, methods, types or classes, imports, calls, routes, configuration access, authorization-like checks, persistence calls, audit/logging calls, and tests. Framework relationships cover common FastAPI, Flask, Django, Express, Fastify, Next.js, and NestJS patterns. The graph classifies anchors as production-reachable, exported entry candidates, test-only, or not reached, and reports unresolved questions such as a missing connected authorization check. Unsupported source is visible as coverage debt and prevents an unmatched source objective from being presented as fully evaluated.

Saved results include a stable fingerprint, repository-relative path, line number, file kind, matched terms, report-safe symbol metadata, imports, relationships, reachability, and unresolved questions—not source excerpts. The technical-evidence schema is versioned independently, and the pack version, SHA-256 content digest, source edition, scan identity, target, and discovery scope remain visible for reproducibility. Go uses the standard-library parser; Python, JavaScript, and TypeScript use dependency-free conservative structural indexers. Dynamic dispatch, runtime route registration, and other source languages remain explicit coverage debt for technical-objective context, while deterministic rules retain their separately documented multi-language coverage.

This technical pack is deliberately not a complete regulatory catalog. The future dashboard will own documentary, organisational, operational, and attestation objectives and combine them with the same stable CLI objective IDs. Its authoritative source is the [official EUR-Lex text](https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32024R1689).

When Ollama is explicitly enabled, ComplyScan makes a second, separate technical-objective review request. It sends only candidates already created by deterministic matching, their bounded graph relationships, and small connected symbol excerpts. Returned strengths (`strong`, `partial`, `weak`, `uncertain`, or `not_supported`) remain bound to the supplied objective ID and evidence fingerprint. They cannot create an objective, change its status, decide legal applicability, or affect the exit code.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Scan completed and no finding met the configured failure threshold. |
| `1` | One or more findings met the configured failure threshold. |
| `2` | The target could not be scanned or configuration/CLI input was invalid. |

The default failure threshold is `high`. `--severity` filters report output; it does not change `fail-on`.

Technical-objective results do not change the process exit code. `framework assess` exits `0` after a valid technical scan even when evidence is not detected, and exits `2` for invalid configuration, pack, or discovery failures. A future dashboard policy gate must remain separate from evidence matching.

## Configuration

`complyscan setup` is safe to use with an existing `.complyscan.yml`: it updates the matching system profile and AI settings while preserving unrelated scan, rule, baseline, and suppression settings. The lower-level `complyscan init` command creates a new config and refuses to overwrite it unless `--force` is passed; `complyscan profile setup` only manages system profiles. A scan loads the config in its target root, or a specific file supplied with `--config`.

```yaml
version: 1

scan:
  exclude:
    - node_modules
    - vendor
    - dist
    - build
    - .complyscan/reports
  max-files: 25000
  max-total-bytes: 104857600
  include-nested-repositories: false
  tracked-only: false

fail-on: high

rules:
  AI-DISC-001:
    enabled: true
  AI-LOG-001:
    enabled: true
  AI-SEC-001:
    enabled: true
  AI-DOC-001:
    enabled: true
  AI-RISK-001:
    enabled: true

ai:
  provider: none
  ollama:
    endpoint: http://127.0.0.1:11434
    model: qwen3:8b
    timeout-seconds: 360
    max-findings: 20

systems:
  - id: candidate-ranking
    name: Candidate ranking
    intended-purpose: Rank job applications for recruiter review.
    lifecycle-stage: development
    organization-roles: [provider]
    operating-regions: [eu, uk]
    use-case-domains: [employment]
    users: [recruiters]
    affected-groups: [job applicants]
    decision-impact: advisory
    human-oversight: required
    data:
      personal-data: yes
      special-category-data: unknown
      children-data: no
    deployment-models: [private-customer, api]
    profile-review:
      status: confirmed
      reviewed-by: A. Reviewer
      reviewed-at: 2026-08-02
    applicability:
      - framework: eu-ai-act
        status: needs-review

baseline: .complyscan-baseline.json

suppressions:
  - rule: AI-LOG-001
    path: testdata/**
    reason: Synthetic prompts are intentionally logged in test fixtures.
```

One repository may declare multiple entries under `systems`. Controlled fields reject unsupported or misspelled values, while users and affected groups remain short factual labels. Confirmed profiles require a named reviewer and date. A human `applicable`, `not-applicable`, or `uncertain` decision additionally requires a rationale; the default is `needs-review`.

The configuration is intended for version control. Do not put secrets, source excerpts, personal records, confidential customer names, or sensitive case details in a profile. Record concise categories and link to access-controlled evidence outside ComplyScan when necessary.

The scanner also respects `.gitignore` and always ignores source-control metadata, common dependency directories, virtual environments, caches, and build output. Binary files, symlinks, and files larger than 1 MiB are not read. Nested Git repositories are skipped unless `--include-nested-repositories` is set.

Discovery is bounded to 25,000 text files and 100 MiB of text by default. Terminal scans report discovery progress every 500 files. Use `--max-files` and `--max-total-bytes` to tune the limits, repeat `--exclude` for temporary exclusions, or use `--tracked-only` to restrict a scan to the Git index. Each option also has a matching key under `scan` in `.complyscan.yml`.

`--changed-since <git-ref>` limits code-level inventory, logging, and secret findings to files changed since that commit or branch. It includes committed changes, staged and unstaged changes, and untracked files. Repository-wide documentation and risk-evidence checks still use the complete discovered repository. This is intentionally a CLI flag rather than persistent configuration because the comparison reference belongs to a particular local or CI run.

Every finding has a stable SHA-256 fingerprint in structured output. Reviewed findings can be suppressed by `rule`, a Git-style `path` pattern, an exact `fingerprint`, or a combination. Every suppression requires a `reason`; suppressed findings are excluded from output and exit-code evaluation, and their count remains visible in the report summary.

For an existing repository, `complyscan baseline .` records the current findings in `.complyscan-baseline.json` without storing source evidence. Commit that deterministic file and future scans will report only findings whose fingerprints are new. Use `--baseline path/to/file` to select another baseline for a scan or `--no-baseline` to inspect every finding.

## Optional Ollama review

The built-in scan default remains deterministic, but interactive `complyscan setup` recommends local Ollama review and performs each installation step only after confirmation. Manual setup remains available:

```bash
ollama serve
ollama pull qwen3:8b
complyscan scan . --review ollama --ollama-model qwen3:8b
```

You can instead set `ai.provider: ollama` in `.complyscan.yml`. `--review none` disables a configured reviewer for one scan. If explicitly enabled review cannot connect, times out, cannot find the model, or returns invalid structured output, the scan exits with code `2` rather than silently omitting the requested review.

The experimental default is the official Ollama `qwen3:8b` model. Ollama does not currently publish a `qwen3-coder:8b` tag; its official Qwen3-Coder family starts at `30b`, which has a substantially larger installation footprint. The model remains configurable. Setup may install Ollama, start its Homebrew service, or pull the selected model only after explicit confirmation; normal scans never install software or models. Fake-transport tests enforce the structured-output, redaction, injection, and identifier-binding contract; a successful live `qwen3:8b` quality and resource run remains required before Ollama review is promoted from experimental status.

ComplyScan uses two separate Ollama requests, both bounded by `max-findings`. The first sends visible, unsuppressed deterministic finding records with re-redacted metadata such as the rule ID, fingerprint, title, message, relative path, line, short evidence, remediation, severity, and confidence. The second sends existing technical candidates with their objective ID, evidence fingerprint, reachability, imports, graph relationships, unresolved questions, and up to six connected source-symbol excerpts. A non-source technical match may include a small excerpt around the matched line. ComplyScan never sends a complete repository or an unbounded file. Input excerpts are re-redacted and are not copied into the saved report.

The endpoint must be `localhost` or a loopback IP; ComplyScan bypasses HTTP proxies and refuses redirects. Repository strings, code, and comments are explicitly labelled untrusted in the fixed prompts. Structured output must preserve submitted finding or objective identifiers; unknown, changed, duplicate, or malformed bindings fail the requested review.

Finding observations are attached to existing fingerprints as `confirmed`, `uncertain`, or `not_supported`. Technical observations use the separate strength vocabulary above and remain attached to the exact objective/evidence pair. Both are advisory: they cannot create or remove findings, change objective status or severity, update baselines or suppressions, or affect the failure threshold. Terminal, Markdown, and JSON keep both review types separate from deterministic results; SARIF carries finding review only. ComplyScan requests non-streaming schema-constrained output with temperature zero, but model output can still vary. See Ollama's [local API](https://docs.ollama.com/api/introduction) and [structured-output documentation](https://docs.ollama.com/capabilities/structured-outputs).

The loopback restriction controls where ComplyScan sends review records. Ollama itself may download models or route specially configured cloud models; select and operate a genuinely local model when a no-cloud boundary is required.

## GitHub Actions

The repository includes a composite action that runs ComplyScan and uploads its SARIF output to GitHub code scanning. The job needs `security-events: write` permission:

```yaml
permissions:
  contents: read
  security-events: write

steps:
  - uses: actions/checkout@v6
    with:
      fetch-depth: 0
  - uses: ComplyScan/ComplyScan@v0.1.2
    with:
      severity: medium
      changed-since: ${{ github.event.pull_request.base.sha }}
```

The `changed-since` input is optional. For pull requests, pass the base commit as shown above and use `fetch-depth: 0` so Git can resolve the comparison and merge base. Omit it for a full scan, such as on a scheduled run or push to the default branch.

By default the action fails after uploading when findings meet `fail-on`. Set `fail-on-findings: false` to publish alerts without failing the job, or `upload-results: false` when code-scanning upload is not available.

The action also accepts `review`, `ollama-model`, and `ollama-endpoint`. Ollama must already be installed, running, and loaded with the selected model inside the job. Hosted GitHub runners cannot access an Ollama service running on a developer's laptop.

## Privacy and security guarantees

In the default deterministic mode, the ComplyScan v0.1.2 CLI:

- runs entirely on the local machine;
- makes no network requests;
- collects no telemetry;
- uploads no source code;
- does not require an API key; and
- never prints a complete detected secret.

`complyscan setup` is a separate provisioning boundary. After explicit confirmations it may invoke Homebrew, download and run Ollama's official Linux installer, start the Homebrew Ollama service, and use `ollama pull` to download the selected model. The setup questionnaire itself makes no network request, profiles are not sent to these installers, and non-interactive setup performs none of those actions unless the corresponding install or pull flag is supplied.

Permission errors are reported as warnings where scanning can safely continue. Source excerpts are short and pass through credential redaction before appearing as evidence.

When Ollama review is explicitly enabled, ComplyScan makes HTTP requests only to the validated loopback endpoint and sends the bounded finding and technical-context records described above. Technical source excerpts exist only in the local request; saved technical evidence contains structural metadata but no source. Model rationales, questions, and suggested actions are re-redacted and length-limited before reporting. Profiles are not sent to Ollama. The generated Markdown and JSON reports remain local unless a future dashboard connection is explicitly enabled.

The optional GitHub Action uploads SARIF metadata to GitHub code scanning when `upload-results` is enabled. That SARIF contains finding messages, repository-relative paths, line numbers, and fingerprints, but not source excerpts or detected credentials. When a job explicitly enables Ollama, SARIF also contains the advisory verdict, confidence, rationale, suggested action, provider, and model for reviewed findings.

## Development

```bash
go mod download
go test ./...
go vet ./...
go build ./cmd/complyscan
```

The pipeline is `repository discovery and file classification → language-neutral graph with Go, Python, JavaScript, and TypeScript indexing → code-only technical-objective matching and bounded context → typed AI inventory → deterministic rules → findings → suppression and filtering → declared applicability context → optional finding and technical-objective Ollama reviews → terminal output plus atomic Markdown and JSON reports`. Add rules by implementing the small `rules.Rule` interface and registering the rule in `rules.DefaultRules`. Repository-wide rules can implement `rules.RepositoryWideRule` to retain full context during changed-since scans. See [the architecture notes](docs/architecture.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

The labelled detector corpus lives under `testdata/evaluation`. Its test reports precision, recall, true positives, false positives, false negatives, and negative cases; the build fails if precision drops below 95% or recall below 90%.

The separate technical-evidence benchmark lives under `testdata/technical-evaluation`. It labels complete objective candidates plus their expected anchor, production/test reachability, required or forbidden graph relationships, and indexed language. Run `./scripts/evaluate-technical-evidence.sh` for a human summary or add `--format json` for a machine-readable result. CI enforces the versioned manifest thresholds. The initial corpus contains repository-shaped Go, Python, and TypeScript services plus a hard negative; it is a regression gate, not a claim of general real-world accuracy. See [the benchmark guide](docs/technical-evidence-benchmark.md).

ComplyScan applies the same evidence discipline to itself. Its maintained [AI applicability assessment](docs/AI_SYSTEM.md) distinguishes the default deterministic mode from the inference-enabled Ollama configuration and records the required pre-release reassessment. The companion [technical risk assessment](docs/risk-assessment.md) records foreseeable harms, controls, residual risks, and review expectations. These documents support governance; they are not self-certification.

## Roadmap

Future releases may add:

- broader labelled coverage for more languages, frameworks, model gateways, and data flows;
- model and AI dependency supply-chain inventory;
- a dashboard catalog that combines CLI code evidence with uploaded documents, declarations, attestations, and operational evidence;
- automatic opt-in synchronization of the exact local JSON evidence bundle;
- graph indexers for additional source languages and framework-specific route/call resolution;
- iterative, still-bounded local context retrieval for unresolved technical questions;
- richer, rule-specific local review prompts and measured live-model evaluation corpora;
- bring-your-own API keys for optional review providers; and
- optional ComplyScan Cloud integrations.

Ollama is the only implemented review provider. The built-in scan default remains `none`; interactive setup recommends Ollama but requires user confirmation before selecting or provisioning it. Paid and remote providers remain placeholders behind the provider interface.

## Disclaimer

ComplyScan findings are automated technical observations. They are not legal advice, a legal determination, a conformity assessment, or compliance certification. Regulatory obligations depend on the complete AI system, its intended purpose, deployment context, actors, data, and real-world use. Obtain qualified legal and compliance review for decisions about the EU AI Act or other laws.

Licensed under the [Apache License 2.0](LICENSE).
