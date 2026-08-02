# ComplyScan

> ComplyScan is a developer-first scanner that identifies potential AI compliance risks and missing governance evidence before code reaches production.

ComplyScan is an open-source, offline-by-default CLI for finding technical signals that deserve review during EU AI Act readiness work. It inventories likely AI providers and frameworks, looks for risky logging and hardcoded credentials, and checks whether repository-level AI-system and risk-classification evidence is present. An explicitly enabled Ollama layer can add local advisory review without changing deterministic findings.

ComplyScan does **not** interpret a complete system, determine an EU AI Act classification, certify compliance, or replace legal and compliance professionals. A finding is a review prompt—not a claim that a system violates the law.

## Install

ComplyScan requires Go 1.22 or newer.

> Development status: the v0.2 functionality documented below is currently unreleased. `v0.1.1` remains the latest tagged release; build `main` to evaluate the development version.

Prebuilt archives for macOS, Linux, and Windows are attached to each [GitHub release](https://github.com/1eonardodawinki/ComplyScan/releases). To install with Go:

```bash
go install github.com/1eonardodawinki/ComplyScan/cmd/complyscan@latest
```

To build from source:

```bash
git clone https://github.com/1eonardodawinki/ComplyScan.git
cd complyscan
go build -o complyscan ./cmd/complyscan
```

Release builds can inject metadata with `-ldflags`:

```bash
go build -ldflags "-X main.version=0.2.0-dev -X main.commit=$(git rev-parse --short HEAD) -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o complyscan ./cmd/complyscan
```

## Quick start

```bash
complyscan init
complyscan scan .
complyscan scan . --format json
complyscan scan . --format sarif > complyscan.sarif
complyscan scan . --severity high --no-color
complyscan scan . --tracked-only
complyscan scan . --exclude fixtures --max-files 10000
complyscan scan . --changed-since main
complyscan scan . --review ollama --ollama-model gemma3
complyscan inventory .
complyscan inventory . --format json
complyscan generate ai-system .
complyscan generate risk-assessment .
complyscan baseline .
complyscan version
```

`scan` defaults to the current directory, so `complyscan scan` and `complyscan scan .` are equivalent.

`inventory` produces a component-focused view rather than compliance findings. It aggregates detected providers and frameworks with their technical evidence, runtime/test/configuration scope, package versions, confidence, and source locations. Its JSON output has a versioned schema for downstream tooling.

The two `generate` commands use that inventory to create `docs/AI_SYSTEM.md` and `docs/risk-assessment.md`. The generated files are deliberately marked as drafts, preserve discovery warnings, and contain explicit human-review fields. Existing documents are protected unless `--force` is supplied.

Terminal scans print findings as rules discover them and finish with the final summary. JSON and SARIF 2.1.0 output remain buffered so they are valid and deterministically ordered. SARIF includes source locations and stable partial fingerprints for code-scanning integrations.

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

The deterministic rule layer is deliberately technical. Legal or regulatory mappings can be added separately without changing the core scanner.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Scan completed and no finding met the configured failure threshold. |
| `1` | One or more findings met the configured failure threshold. |
| `2` | The target could not be scanned or configuration/CLI input was invalid. |

The default failure threshold is `high`. `--severity` filters report output; it does not change `fail-on`.

## Configuration

`complyscan init` creates `.complyscan.yml` and refuses to overwrite it unless `--force` is passed. A scan loads the config in its target root, or a specific file supplied with `--config`.

```yaml
version: 1

scan:
  exclude:
    - node_modules
    - vendor
    - dist
    - build
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
    model: gemma3
    timeout-seconds: 120
    max-findings: 20

baseline: .complyscan-baseline.json

suppressions:
  - rule: AI-LOG-001
    path: testdata/**
    reason: Synthetic prompts are intentionally logged in test fixtures.
```

The scanner also respects `.gitignore` and always ignores source-control metadata, common dependency directories, virtual environments, caches, and build output. Binary files, symlinks, and files larger than 1 MiB are not read. Nested Git repositories are skipped unless `--include-nested-repositories` is set.

Discovery is bounded to 25,000 text files and 100 MiB of text by default. Terminal scans report discovery progress every 500 files. Use `--max-files` and `--max-total-bytes` to tune the limits, repeat `--exclude` for temporary exclusions, or use `--tracked-only` to restrict a scan to the Git index. Each option also has a matching key under `scan` in `.complyscan.yml`.

`--changed-since <git-ref>` limits code-level inventory, logging, and secret findings to files changed since that commit or branch. It includes committed changes, staged and unstaged changes, and untracked files. Repository-wide documentation and risk-evidence checks still use the complete discovered repository. This is intentionally a CLI flag rather than persistent configuration because the comparison reference belongs to a particular local or CI run.

Every finding has a stable SHA-256 fingerprint in structured output. Reviewed findings can be suppressed by `rule`, a Git-style `path` pattern, an exact `fingerprint`, or a combination. Every suppression requires a `reason`; suppressed findings are excluded from output and exit-code evaluation, and their count remains visible in the report summary.

For an existing repository, `complyscan baseline .` records the current findings in `.complyscan-baseline.json` without storing source evidence. Commit that deterministic file and future scans will report only findings whose fingerprints are new. Use `--baseline path/to/file` to select another baseline for a scan or `--no-baseline` to inspect every finding.

## Optional Ollama review

Ollama review is disabled by default. Install Ollama, start its local service, and pull a model before enabling it:

```bash
ollama serve
ollama pull gemma3
complyscan scan . --review ollama --ollama-model gemma3
```

You can instead set `ai.provider: ollama` in `.complyscan.yml`. `--review none` disables a configured reviewer for one scan. If explicitly enabled review cannot connect, times out, cannot find the model, or returns invalid structured output, the scan exits with code `2` rather than silently omitting the requested review.

ComplyScan sends only visible, unsuppressed deterministic finding records to Ollama, up to `max-findings`. Each record contains bounded and re-redacted metadata such as the rule ID, fingerprint, title, message, relative path, line, short evidence, remediation, severity, and confidence. It does not send complete repository files. The endpoint must be `localhost` or a loopback IP; ComplyScan bypasses HTTP proxies and refuses redirects.

Ollama observations are attached to existing fingerprints as `confirmed`, `uncertain`, or `not_supported`. They are advisory: they cannot create or remove findings, change severity, update baselines or suppressions, or affect the failure threshold. Terminal, JSON, and SARIF keep them separate from deterministic results. ComplyScan requests non-streaming schema-constrained output with temperature zero, but model output can still vary. See Ollama's [local API](https://docs.ollama.com/api/introduction) and [structured-output documentation](https://docs.ollama.com/capabilities/structured-outputs).

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
  - uses: 1eonardodawinki/ComplyScan@main # unreleased development version
    with:
      severity: medium
      changed-since: ${{ github.event.pull_request.base.sha }}
```

The `changed-since` input is optional. For pull requests, pass the base commit as shown above and use `fetch-depth: 0` so Git can resolve the comparison and merge base. Omit it for a full scan, such as on a scheduled run or push to the default branch.

By default the action fails after uploading when findings meet `fail-on`. Set `fail-on-findings: false` to publish alerts without failing the job, or `upload-results: false` when code-scanning upload is not available.

The action also accepts `review`, `ollama-model`, and `ollama-endpoint`. Ollama must already be installed, running, and loaded with the selected model inside the job. Hosted GitHub runners cannot access an Ollama service running on a developer's laptop.

## Privacy and security guarantees

In the default deterministic mode, the ComplyScan v0.2 development CLI:

- runs entirely on the local machine;
- makes no network requests;
- collects no telemetry;
- uploads no source code;
- does not require an API key; and
- never prints a complete detected secret.

Permission errors are reported as warnings where scanning can safely continue. Source excerpts are short and pass through credential redaction before appearing as evidence.

When Ollama review is explicitly enabled, ComplyScan makes HTTP requests only to the validated loopback endpoint and sends the bounded finding records described above. Model rationales and suggested actions are also re-redacted and length-limited before reporting.

The optional GitHub Action uploads SARIF metadata to GitHub code scanning when `upload-results` is enabled. That SARIF contains finding messages, repository-relative paths, line numbers, and fingerprints, but not source excerpts or detected credentials. When a job explicitly enables Ollama, SARIF also contains the advisory verdict, confidence, rationale, suggested action, provider, and model for reviewed findings.

## Development

```bash
go mod download
go test ./...
go vet ./...
go build ./cmd/complyscan
```

The pipeline is `repository discovery → file classification → typed AI inventory → deterministic rules → findings → suppression and filtering → optional advisory review → report`. Add rules by implementing the small `rules.Rule` interface and registering the rule in `rules.DefaultRules`. Repository-wide rules can implement `rules.RepositoryWideRule` to retain full context during changed-since scans. See [the architecture notes](docs/architecture.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

The labelled detector corpus lives under `testdata/evaluation`. Its test reports precision, recall, true positives, false positives, false negatives, and negative cases; the build fails if precision drops below 95% or recall below 90%.

ComplyScan applies the same evidence discipline to itself. Its maintained [AI applicability assessment](docs/AI_SYSTEM.md) distinguishes the default deterministic mode from the inference-enabled Ollama configuration and records the required pre-release reassessment. The companion [technical risk assessment](docs/risk-assessment.md) records foreseeable harms, controls, residual risks, and review expectations. These documents support governance; they are not self-certification.

## Roadmap

Future releases may add:

- broader labelled coverage for more languages, frameworks, model gateways, and data flows;
- model and AI dependency supply-chain inventory;
- versioned evidence packs and traceable regulatory mappings;
- richer, rule-specific local review prompts and model evaluation fixtures;
- bring-your-own API keys for optional review providers; and
- optional ComplyScan Cloud integrations.

Ollama is the only implemented review provider and is never selected automatically by the built-in defaults. Paid and remote providers remain placeholders behind the provider interface.

## Disclaimer

ComplyScan findings are automated technical observations. They are not legal advice, a legal determination, a conformity assessment, or compliance certification. Regulatory obligations depend on the complete AI system, its intended purpose, deployment context, actors, data, and real-world use. Obtain qualified legal and compliance review for decisions about the EU AI Act or other laws.

Licensed under the [Apache License 2.0](LICENSE).
