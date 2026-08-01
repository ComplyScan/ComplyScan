# ComplyScan

> ComplyScan is a developer-first scanner that identifies potential AI compliance risks and missing governance evidence before code reaches production.

ComplyScan is an open-source, offline CLI for finding technical signals that deserve review during EU AI Act readiness work. It inventories likely AI providers and frameworks, looks for risky logging and hardcoded credentials, and checks whether repository-level AI-system and risk-classification evidence is present.

ComplyScan does **not** interpret a complete system, determine an EU AI Act classification, certify compliance, or replace legal and compliance professionals. A finding is a review prompt—not a claim that a system violates the law.

## Install

ComplyScan requires Go 1.22 or newer.

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
go build -ldflags "-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o complyscan ./cmd/complyscan
```

## Quick start

```bash
complyscan init
complyscan scan .
complyscan scan . --format json
complyscan scan . --severity high --no-color
complyscan scan . --tracked-only
complyscan scan . --exclude fixtures --max-files 10000
complyscan version
```

`scan` defaults to the current directory, so `complyscan scan` and `complyscan scan .` are equivalent.

Terminal scans print findings as rules discover them and finish with the final summary. JSON output remains buffered so it is valid and deterministically ordered.

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
| `AI-DISC-001` | Info | Inventory likely AI providers and frameworks, including OpenAI, Anthropic, Gemini, Mistral, Cohere, Hugging Face, Ollama, LiteLLM, LangChain, LlamaIndex, Vercel AI SDK, and OpenRouter. Results are aggregated by provider with representative locations. |
| `AI-LOG-001` | High | Flag conservative cases where prompt-like or response-like values appear in common Go, Python, JavaScript, or TypeScript logging calls. |
| `AI-SEC-001` | High | Detect likely hardcoded AI API credentials and report only redacted evidence. |
| `AI-DOC-001` | Medium | When AI usage is detected, check for a model card or AI-system documentation. |
| `AI-RISK-001` | Medium | When AI usage is detected, check for repository-level AI risk-classification evidence. |

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
```

The scanner also respects `.gitignore` and always ignores source-control metadata, common dependency directories, virtual environments, caches, and build output. Binary files, symlinks, and files larger than 1 MiB are not read. Nested Git repositories are skipped unless `--include-nested-repositories` is set.

Discovery is bounded to 25,000 text files and 100 MiB of text by default. Terminal scans report discovery progress every 500 files. Use `--max-files` and `--max-total-bytes` to tune the limits, repeat `--exclude` for temporary exclusions, or use `--tracked-only` to restrict a scan to the Git index. Each option also has a matching key under `scan` in `.complyscan.yml`.

## Privacy and security guarantees

ComplyScan v0.1:

- runs entirely on the local machine;
- makes no network requests;
- collects no telemetry;
- uploads no source code;
- does not require an API key; and
- never prints a complete detected secret.

Permission errors are reported as warnings where scanning can safely continue. Source excerpts are short and pass through credential redaction before appearing as evidence.

## Development

```bash
go mod download
go test ./...
go vet ./...
go build ./cmd/complyscan
```

The pipeline is `repository discovery → file classification → deterministic rules → findings → report`. Add rules by implementing the small `rules.Rule` interface and registering the rule in `rules.DefaultRules`; the scanner itself does not need rule-specific logic. See [the architecture notes](docs/architecture.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

## Roadmap

Future releases may add:

- local AI review through Ollama;
- bring-your-own API keys for optional providers;
- SARIF output and pull-request annotations;
- more languages and AI frameworks;
- additional regulatory mappings; and
- optional ComplyScan Cloud integrations.

Provider names in v0.1 are placeholders behind an interface; no paid or network provider is implemented or selected automatically.

## Disclaimer

ComplyScan findings are automated technical observations. They are not legal advice, a legal determination, a conformity assessment, or compliance certification. Regulatory obligations depend on the complete AI system, its intended purpose, deployment context, actors, data, and real-world use. Obtain qualified legal and compliance review for decisions about the EU AI Act or other laws.

Licensed under the [Apache License 2.0](LICENSE).
