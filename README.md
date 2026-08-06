# ComplyScan

> ComplyScan is a developer-first scanner that identifies potential AI compliance risks and missing governance evidence before code reaches production.

ComplyScan is an open-source, offline-by-default CLI for finding technical signals that deserve review during EU AI Act readiness work. Guided setup records factual system context and attributable human applicability decisions; a versioned technical pack maps code and configuration signals to EU AI Act technical objectives; and scans inventory likely AI providers and frameworks, look for risky logging and hardcoded credentials, and check whether repository-level AI-system and risk-classification evidence is present. An explicitly enabled Ollama layer can add a local, bounded evidence investigation of deterministic candidates and likely-required objectives for which no candidate was detected, without changing deterministic evidence or legal applicability.

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

`setup` is the recommended first command. It creates or updates `.complyscan.yml`, runs the factual system and human-applicability questionnaire, recommends local Ollama review, lets the user choose any Ollama model, offers to install and start Ollama when it is missing, offers to pull the selected model, and can run the first scan. Every interactive question first explains why the fact matters, defines each controlled option in developer language, and provides examples where useful. The wizard recommends `needs-review` rather than asking developers to invent a legal conclusion. Each software installation and model download requires a separate confirmation. If setup cannot finish an external installation or download, it still saves the collected repository configuration and prints the exact recovery command.

`doctor` performs an offline readiness check for the installed build, repository configuration, Git detection, report-directory permissions, the Ollama executable, its loopback service, and the configured model. Missing optional tools are warnings; a required but unavailable Ollama service or model is a blocking failure.

Maintainers can run the repeatable live-model quality and resource gate with `./scripts/validate-ollama.sh` after `qwen3:8b` is available. See [Ollama live-model validation](docs/ollama-validation.md) for the enforced production/test-only expectations and saved artifacts.

Interactive setup selects `qwen3:8b` by default, but the user can enter another local model or decline Ollama and keep deterministic scanning. Automation never installs software or downloads a model unless `--install-ollama` or `--pull-model` is explicitly passed. Use `--non-interactive --review none` for a network-free starter configuration, or `--skip-ollama-install`, `--skip-model-pull`, and `--skip-scan` to control individual interactive steps.

`scan` defaults to the current directory, so `complyscan scan` and `complyscan scan .` are equivalent. In a terminal, `init` guides you through factual questions about each system's purpose, operating regions, value-chain role, use-case domain, users, affected groups, decision impact, AI activities, data, human oversight, and deployment. AI activities distinguish inference, training, fine-tuning, evaluation, automated decisions, agent tool use, and synthetic-content generation. Use `unknown` rather than guessing. Redirected or CI initialization is non-interactive and can be made explicit with `--non-interactive`.

`profile show` reports conservative EU AI Act scope and high-risk screening from those declared facts. `profile setup` adds a system to an existing config; use `--replace` to update a profile with the same ID. Automated screening, human decisions, and missing context remain separate. ComplyScan never converts a scope signal into a compliance certificate.

`inventory` produces a component-focused view rather than compliance findings. It aggregates detected providers and frameworks with their technical evidence, runtime/test/configuration scope, package versions, confidence, and source locations. Its JSON output has a versioned schema for downstream tooling.

The two `generate` commands use that inventory to create `docs/AI_SYSTEM.md` and `docs/risk-assessment.md`. The generated files are deliberately marked as drafts, preserve discovery warnings, and contain explicit human-review fields. Existing documents are protected unless `--force` is supplied.

Terminal scans print findings as rules discover them and finish with declared applicability context, the independently discovered AI component inventory, requirement-to-evidence reconciliation, code-only technical evidence, and the final summary. Reconciliation distinguishes likely requirements with candidate evidence, likely requirements without detected evidence, configuration/evidence mismatches, unresolved applicability, and repository evidence that cannot safely be assigned to a configured system. When Ollama is enabled, reconciliation also shows the strongest advisory assurance reached for each investigated objective. “Without detected evidence” and “not found after investigation” remain bounded search statements, never proof of absence or breach.

Every successful scan atomically writes `.complyscan/reports/latest.md` for people and `.complyscan/reports/latest.json` as the versioned machine-readable evidence bundle. The schema-version 3 bundle includes the inputs, deterministic reconciliation, grounded investigation claims, assurance level, and explicit runtime/legal-review boundaries so a future dashboard can display or recompute the mapping. `complyscan init` adds that generated directory to `.gitignore`; scans always exclude it from subsequent discovery. Use `--no-report` to disable persistence or `--report-dir` to select another directory inside the target.

JSON and SARIF 2.1.0 output remain buffered so they are valid and deterministically ordered. JSON stdout and `latest.json` include applicability, AI inventory, technical evidence, and reconciliation. SARIF includes source locations and stable partial fingerprints for code-scanning integrations, but omits technical-objective summaries that have no single code-scanning location.

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

The built-in `eu-ai-act-technical-evidence` pack version `0.1.2` contains 13 code objectives associated with Articles 9, 10, 12, 14, 15, and 50 of Regulation (EU) 2024/1689. They cover technical signals for risk-control tests, dataset validation, bias evaluation, automatic logging, retention configuration, human review, override and stop mechanisms, performance thresholds, robustness, AI-specific security, interaction disclosure, and synthetic-content marking. Version 0.1.2 retains the evidence-matching refinements measured against pinned public repositories and moves every deterministic applicability condition into the same strictly validated YAML objective.

The complete inspectable pack is [eu-ai-act-technical-evidence-v0.1.2.yml](internal/framework/packs/eu-ai-act-technical-evidence-v0.1.2.yml). Each objective declares its source Article, description, applicability note, legal scope, relevant configured AI activities, external-use condition, eligible file kinds, evidence signals, and required verification. Reconciliation consumes those YAML fields directly; there is no second hard-coded objective mapping in Go.

The pack accepts only source, configuration, dependency, test, CI, container, and infrastructure file kinds. It does not contain objectives for policies, risk assessments, training records, conformity documents, attestations, or other evidence intended for a future dashboard. The CLI runs technical checks even when no system profile exists; declared context remains a separate, provisional report section rather than an activation gate. Applicability conditions prioritize and reconcile objectives but never suppress independent repository discovery.

Every technical objective receives one of these statuses:

| Status | Meaning |
| --- | --- |
| `candidate-evidence` | One or more eligible files matched all configured signal groups. The code still requires technical and human verification. |
| `not-detected` | The bounded scan did not locate the configured technical signal. This does not prove the implementation is absent. |
| `not-evaluated` | The technical check could not be evaluated. This status is reserved for explicit limitations rather than treated as a failure. |

Evidence matching requires every configured keyword group and path signal to match one eligible file. A language-neutral repository graph then attaches bounded structural context to each candidate. The current indexers support Go, Python, JavaScript, and TypeScript and record functions, methods, types or classes, imports, calls, routes, configuration access, authorization-like checks, persistence calls, audit/logging calls, and tests. Framework relationships cover common FastAPI, Flask, Django, Express, Fastify, Next.js, and NestJS patterns. The graph classifies anchors as production-reachable, exported entry candidates, test-only, or not reached, and reports unresolved questions such as a missing connected authorization check. Unsupported source is visible as coverage debt and prevents an unmatched source objective from being presented as fully evaluated.

Saved results include a stable fingerprint, repository-relative path, line number, file kind, matched terms, report-safe symbol metadata, imports, relationships, reachability, and unresolved questions—not source excerpts. The technical-evidence schema is versioned independently, and the pack version, SHA-256 content digest, source edition, scan identity, target, and discovery scope remain visible for reproducibility. Go uses the standard-library parser; Python, JavaScript, and TypeScript use dependency-free conservative structural indexers. Dynamic dispatch, runtime route registration, and other source languages remain explicit coverage debt for technical-objective context, while deterministic rules retain their separately documented multi-language coverage.

This technical pack is deliberately not a complete regulatory catalog. The future dashboard will own documentary, organisational, operational, and attestation objectives and combine them with the same stable CLI objective IDs. Its authoritative source is the [official EUR-Lex text](https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32024R1689).

When Ollama is explicitly enabled, ComplyScan performs a separate technical evidence investigation after deterministic matching. Existing candidates receive bounded graph relationships and connected symbol excerpts. A likely-required objective with no candidate receives a separate extended-search target containing eligible-file coverage, ranked repository excerpts, and a bounded manifest. Before its final decision, the model may request one follow-up round containing at most three literal search terms and repository-relative path hints. Trusted code—not the model—searches only already eligible discovered files and returns at most three bounded excerpts. Globs, regular expressions, traversal, absolute paths, commands, filesystem access, and additional rounds are rejected or safely skipped. The model receives no evidence fingerprint and returns no binding identifiers; trusted ComplyScan code attaches the sole decision to the submitted objective and fingerprint. It must separate supporting, contradictory, and missing evidence and may cite only paths included in the submitted context. Trusted code derives the final assurance level from the guarded strength and reachability. Neither the investigation nor its assurance can change objective status, decide legal applicability, establish runtime effectiveness, or affect the exit code.

Investigation conclusions distinguish `technically-substantiated`, `partially-substantiated`, `test-only-evidence`, `unreachable-evidence`, `not-substantiated`, `not-found-after-investigation`, and `cannot-determine`. Assurance levels distinguish a detected signal, AI-substantiated evidence, structurally verified repository evidence, observed test evidence, an extended investigation with no evidence, and an unresolved investigation. Operational proof and legal acceptance are intentionally outside these levels.

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
    ai-activities: [inference, automated-decision]
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

Technical investigations are cached in the operating system's private user-cache directory, not in the scanned repository. The cache stores the same bounded, redacted observation used in reports plus hashes of submitted context; it does not store the submitted source-context records. Model rationales and evidence summaries are instructed not to quote code, but may still describe repository details and should be treated as potentially sensitive local data. Reuse requires the same provider, model tag, prompt version, control-pack ID/version/digest, objective, evidence fingerprint, complete bounded input digest, and full discovered-repository digest. Any repository code, context, pack, prompt, or model-name change therefore triggers a new request. Use `--refresh-review` to deliberately bypass existing technical observations. Each scan prints whether an investigation target is being reviewed by Ollama or reused from cache; completed responses are saved individually so an interrupted long scan can resume.

The experimental default is the official Ollama `qwen3:8b` model. Ollama does not currently publish a `qwen3-coder:8b` tag; its official Qwen3-Coder family starts at `30b`, which has a substantially larger installation footprint. The model remains configurable. Setup may install Ollama, start its Homebrew service, or pull the selected model only after explicit confirmation; normal scans never install software or models. Fake-transport tests enforce the structured-output, redaction, injection, identifier-binding, grounded-path, and bounded-retrieval contracts. A two-target prompt-version-7 smoke run passed on 2026-08-06, including one successful three-excerpt follow-up and one malformed optional plan safely skipped before the expected bounded-negative result. Broader representative evaluation and the maintained applicability reassessment remain open before Ollama investigation can leave experimental status.

ComplyScan uses two separate review flows, both bounded by `max-findings`. The finding flow sends visible, unsuppressed deterministic finding records with re-redacted metadata such as the rule ID, fingerprint, title, message, relative path, line, short evidence, remediation, severity, and confidence. The technical flow can make a search-planning request followed by one final decision request per existing candidate or likely-required missing-evidence investigation target. Candidate targets include reachability, imports, graph relationships, unresolved questions, a bounded match window, and connected symbol excerpts. Extended-search targets include deterministic search terms, coverage counts, up to six ranked excerpts, and at most 200 eligible repository paths. A requested follow-up can add at most three 2,000-character excerpts. The evidence fingerprint is withheld. ComplyScan never sends a complete repository or an unbounded file. Input excerpts are re-redacted and are not copied into the saved report. Investigating many objectives can therefore take materially longer than one batched model call.

The endpoint must be `localhost` or a loopback IP; ComplyScan bypasses HTTP proxies and refuses redirects. Repository strings, code, and comments are explicitly labelled untrusted in the fixed prompts. Finding output must preserve submitted identifiers; unknown, changed, duplicate, or malformed bindings fail that review. Technical output contains only decision and evidence-summary fields, so the model cannot choose which objective receives its response. Any cited path outside the submitted bounded context fails the requested investigation.

Finding observations are attached to existing fingerprints as `confirmed`, `uncertain`, or `not_supported`. Technical investigations remain attached to the exact objective/evidence target. Both are advisory: they cannot create or remove findings, change objective status or severity, update baselines or suppressions, or affect the failure threshold. Terminal, Markdown, and schema-version 3 JSON keep review separate from deterministic results; SARIF carries finding review only. ComplyScan requests non-streaming schema-constrained output with temperature zero, but model output can still vary. See Ollama's [local API](https://docs.ollama.com/api/introduction) and [structured-output documentation](https://docs.ollama.com/capabilities/structured-outputs).

Deterministic, transparent guardrails constrain two recurring reasoning failures: discussion-only blog, documentation, FAQ, example, and quiz components cannot become implementation evidence, while executable graders, rubric renderers, assertions, and evaluation templates remain reviewable evidence even when static analysis cannot resolve dynamic registration. The report preserves the model's original strength in `model_strength` whenever a guardrail changes it.

The loopback restriction controls where ComplyScan sends review records. Ollama itself may download models or route specially configured cloud models; select and operate a genuinely local model when a no-cloud boundary is required.

## Optional isolated execution verification

ComplyScan can run a user-selected test command in a preloaded local Docker or Podman image and attach the bounded, redacted result to terminal, Markdown, and JSON reports. This is always opt-in. The repository is mounted read-only; the container has no network, no added capabilities, a read-only root filesystem, bounded CPU, memory, processes, temporary storage, output, and time; and the command runs directly without a shell. ComplyScan never pulls the image. The image itself remains a user-selected trust boundary.

```bash
docker pull golang:1.25
complyscan scan . \
  --verify-image golang:1.25 \
  --verify-command go \
  --verify-arg test \
  --verify-arg ./... \
  --verify-objective eu-aia-15-robustness-failure-handling
```

Use `--verify-runtime podman` when preferred, repeat `--verify-arg` and `--verify-objective` as needed, and set `--verify-timeout` up to 30 minutes. Objective association is explicitly user-declared. A passing command is supporting execution evidence only; it does not prove that the test covers the objective, that the control is operationally effective, or that the system complies with the Act. A test failure is recorded in the report and does not change ComplyScan's finding-threshold exit code; container/runtime/configuration failures exit with code `2`.

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

When Ollama review is explicitly enabled, ComplyScan makes HTTP requests only to the validated loopback endpoint and sends the bounded finding and technical-context records described above. Submitted technical source-context records exist only in the local request and are not stored in the evidence bundle or OS user-cache. Model rationales, questions, and suggested actions may describe repository details; they are re-redacted and length-limited before reporting or caching. Profiles are not sent to Ollama. The generated Markdown and JSON reports and cached observations remain local unless a future dashboard connection is explicitly enabled.

The optional GitHub Action uploads SARIF metadata to GitHub code scanning when `upload-results` is enabled. That SARIF contains finding messages, repository-relative paths, line numbers, and fingerprints, but not source excerpts or detected credentials. When a job explicitly enables Ollama, SARIF also contains the advisory verdict, confidence, rationale, suggested action, provider, and model for reviewed findings.

## Development

```bash
go mod download
go test ./...
go vet ./...
go build ./cmd/complyscan
```

The pipeline has two independent inputs: declared system configuration determines which technical objectives are likely relevant, while repository discovery builds the AI inventory and technical evidence without trusting that configuration. A deterministic reconciliation layer then combines them, preserves mismatches and uncertainty, and refuses to guess evidence ownership when a repository declares multiple systems. Optional Ollama review annotates existing evidence afterward and cannot alter applicability, deterministic evidence, or reconciliation status. See [the architecture notes](docs/architecture.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

The labelled detector corpus lives under `testdata/evaluation`. Its test reports precision, recall, true positives, false positives, false negatives, and negative cases; the build fails if precision drops below 95% or recall below 90%.

The separate technical-evidence benchmark lives under `testdata/technical-evaluation`. It labels complete objective candidates plus their expected anchor, production/test reachability, required or forbidden graph relationships, and indexed language. Run `./scripts/evaluate-technical-evidence.sh` for a human summary or add `--format json` for a machine-readable result. CI enforces the versioned synthetic thresholds. A manual `./scripts/evaluate-external-repositories.sh` study fetches exact commits of three permissively licensed public AI repositories and checks source-free human labels without committing third-party code. Add `--review ollama` for the slower `qwen3:8b` semantic gate over those candidates. These are regression gates and tuning evidence, not claims of general real-world accuracy. See [the benchmark guide](docs/technical-evidence-benchmark.md).

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
