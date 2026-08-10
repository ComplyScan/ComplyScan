# ComplyScan

> ComplyScan is a developer-first scanner that identifies potential AI compliance risks and missing governance evidence before code reaches production.

ComplyScan is an open-source, offline-by-default CLI for finding technical signals that deserve review against selected AI governance sources. Guided setup records factual system context, lets the operator select technical evidence packs, and collects attributable human applicability decisions where legislation requires them. A framework-neutral control layer maps code and configuration signals to both EU AI Act technical objectives and voluntary NIST AI RMF practices. Scans also inventory likely AI providers and frameworks, look for risky logging and hardcoded credentials, and check whether repository-level AI-system and risk-classification evidence is present. An explicitly enabled model layer can add a bounded evidence investigation without changing deterministic evidence, legal applicability, or voluntary-framework status. Ollama keeps that context local; optional OpenAI, Anthropic, and Gemini adapters send only the bounded redacted context after explicit consent using the operator's own API key.

ComplyScan does **not** interpret a complete system, determine an EU AI Act classification, certify compliance, or replace legal and compliance professionals. A finding is a review prompt—not a claim that a system violates the law.

## Install

> Current release: v0.1.5. Model review is optional and experimental; deterministic scanning remains the default.

macOS and Linux users can install ComplyScan and immediately start guided setup with one command:

```bash
curl -fsSL https://complyscan.github.io/ComplyScan/install.sh | sh
```

The installer is published from this repository through GitHub Pages. It detects the operating system and CPU architecture, downloads the matching release archive, verifies its published SHA-256 checksum, installs `complyscan` into `~/.local/bin`, and launches `complyscan setup` through the terminal. It does not use `sudo`. Pass `--no-setup` for automation or pin both the installer and binary version:

```bash
curl -fsSL https://github.com/ComplyScan/ComplyScan/releases/download/v0.1.5/install.sh | sh -s -- --version v0.1.5 --no-setup
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
go build -ldflags "-X main.version=0.1.5 -X main.commit=$(git rev-parse --short HEAD) -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o complyscan ./cmd/complyscan
```

## Quick start

```bash
complyscan # setup when needed; otherwise scan the current repository
complyscan setup
complyscan setup --advanced
complyscan init
complyscan profile show
complyscan profile setup # add context to an existing configuration
complyscan ownership setup # map code paths when the repo contains multiple systems
complyscan ownership show
complyscan framework list
complyscan framework assess .
complyscan framework assess . --pack nist-ai-rmf-technical-evidence
complyscan framework assess . --format json
complyscan scan .
complyscan scan . --quick
complyscan scan . --deep
complyscan scan . --verbose
complyscan scan . --format json
complyscan scan . --format sarif > complyscan.sarif
complyscan scan . --severity high --no-color
complyscan scan . --tracked-only
complyscan scan . --exclude fixtures --max-files 10000
complyscan scan . --changed-since main
complyscan scan . --review ollama --ollama-model qwen3.5:9b
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

Running `complyscan` is the recommended first command. With no configuration it first asks the operator to choose local AI, explicitly consented cloud AI, or fast model-free analysis. It then discovers the repository once, summarizes languages and detected AI components, and—when a ready model was selected—requests a bounded, secret-redacted profile draft before showing factual questions. Suggested answers display confidence, rationale, and cited repository paths; pressing Enter confirms the editable default, while the operator can change it or retain `unknown`. Jurisdiction, organisation role, actual production use, legal applicability, and legal risk class are never inferred. After recommending a framework mapping, setup asks only the remaining relevant technical, people, and data facts. `complyscan setup --advanced` remains available for the complete unbranched profile and an attributed human applicability decision.

In an interactive terminal, fixed single-answer questions use an arrow-controlled radio menu, multiple-answer questions use checkboxes, and confirmations show a highlighted Yes/No choice. Move with ↑/↓, tick or untick with Space where applicable, and press Enter to confirm. Local and remote model pickers use the same menus and include an explicit custom-model option before asking for free text. These controls also cover the system, test-command, and objective choices in verification setup. Redirected setup scripts keep the stable numbered or text interface. Set `COMPLYSCAN_ACCESSIBLE=1` to force that interface for screen readers or terminals where cursor-based controls are unsuitable.

The EU setup ends with an applicability-readiness gate. `incomplete` means one or more facts used by the current technical mapping are still unknown; `factually-ready` means those inputs are present but have no named factual reviewer; `human-reviewed` means the factual profile has a named reviewer. These statuses describe input quality only. They do not decide that the EU AI Act applies and do not replace the separate legal applicability decision, which remains `needs-review` unless an accountable human records otherwise. An incomplete profile never prevents repository discovery: the scan continues and labels its requirement mapping as provisional.

First-run setup finishes by offering a quick deterministic scan, a deep AI-assisted review, or saving configuration without scanning. Deep is the default only after a selected reviewer is ready; otherwise quick remains the default and never invokes a model. The final scan reuses the repository discovery already completed for onboarding instead of reading and classifying every file again. Ollama setup lists installed models plus recommendations, accepts any exact tag, and offers installation or model download separately. Remote setup explains external processing and possible cost, asks for the model, and saves only the API-key environment-variable name.

`doctor` checks the installed build, repository configuration, Git detection, report-directory permissions, local Ollama readiness, and the presence—not the value—of a configured remote credential. `complyscan doctor --probe-review` makes a separate live synthetic structured-output request; remote probes may incur a small provider charge and never contain repository data.

Maintainers can run the repeatable live-model quality and resource gate with `./scripts/validate-ollama.sh` after `qwen3.5:9b` is available. See [Ollama live-model validation](docs/ollama-validation.md) for the enforced production/test-only expectations and saved artifacts.

Interactive setup selects `qwen3.5:9b` as the recommended local default candidate, lists the previously validated `qwen3:8b` option and other installed models, and accepts any Ollama tag. The new default must pass the maintained live gate before it inherits the previous model's validation claim. Automation never installs software or downloads a model unless `--install-ollama` or `--pull-model` is explicitly passed. Non-interactive remote setup additionally requires `--allow-remote-review`; use `--non-interactive --review none` for a network-free starter configuration.

`scan` defaults to the current directory, so `complyscan scan` and `complyscan scan .` are equivalent. Routine scans are deterministic and fast even when a reviewer is configured; `--quick` makes that intent explicit, while `--deep` requires and invokes the configured reviewer. Passing an explicit one-off `--review` also invokes that provider. Findings still print as they are discovered. The default completion is concise, while `--verbose` prints every framework objective and model observation in the terminal. Use `unknown` rather than guessing. Redirected or CI initialization is non-interactive and can be made explicit with `--non-interactive`.

`profile show` reports conservative EU AI Act scope, high-risk screening, technical-mapping readiness, and unresolved facts from the declared profile. `profile setup` adds a system to an existing config; use `--replace` to update a profile with the same ID. When a repository declares multiple systems, setup offers a separate path-ownership wizard. `ownership setup` can replace those mappings later without repeating the applicability questionnaire, and `ownership show` prints them as terminal text or JSON. Automated screening, factual readiness, human decisions, and missing context remain separate. ComplyScan never converts a scope signal into a compliance certificate.

`inventory` produces a component-focused view rather than compliance findings. It aggregates detected providers and frameworks with their technical evidence, runtime/test/configuration scope, package versions, confidence, and source locations. Its JSON output has a versioned schema for downstream tooling.

The two `generate` commands use that inventory to create `docs/AI_SYSTEM.md` and `docs/risk-assessment.md`. The generated files are deliberately marked as drafts, preserve discovery warnings, and contain explicit human-review fields. Existing documents are protected unless `--force` is supplied.

Terminal scans print findings as rules discover them and finish with a concise inventory, technical-objective, requirement-mapping, and advisory-review summary. Full framework detail remains in the Markdown and JSON reports or can be printed with `--verbose`. EU reconciliation distinguishes likely requirements, gaps, mismatches, and unresolved applicability. NIST reconciliation labels selected practices as recommendations, never legal requirements. Repository evidence that cannot safely be assigned to a configured system remains unresolved. When model review is enabled, reconciliation also shows the strongest advisory assurance reached for each investigated objective. “Without detected evidence” and “not found after investigation” remain bounded search statements, never proof of absence or breach.

Every successful scan atomically writes `.complyscan/reports/latest.md` for people and `.complyscan/reports/latest.json` as the versioned machine-readable evidence bundle. A deep scan writes the complete preliminary report before its first model request and checkpoints completed framework reviews, so provider failures cannot erase deterministic results. Invalid output for one technical target is recorded as incomplete while later targets continue. The schema-version 5 bundle contains a `frameworks` array with each pack's identity, nature, applicability where relevant, technical evidence, reconciliation, and optional grounded investigation. It also includes path ownership, assurance, and explicit runtime/legal-review boundaries so a future dashboard can display or recompute the mapping. Transitional EU-only top-level fields remain populated for compatibility and will be removed only in a future breaking schema version. `complyscan init` adds the generated directory to `.gitignore`; scans always exclude it from subsequent discovery. Use `--no-report` to disable persistence or `--report-dir` to select another directory inside the target.

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

ComplyScan currently embeds two inspectable code-only packs:

- [EU AI Act technical evidence v0.1.3](internal/framework/packs/eu-ai-act-technical-evidence-v0.1.3.yml) contains 13 objectives associated with Articles 9, 10, 12, 14, 15, and 50 of Regulation (EU) 2024/1689. These are screened against declared context and kept separate from the attributed human applicability decision.
- [NIST AI RMF technical evidence v0.1.0](internal/framework/packs/nist-ai-rmf-technical-evidence-v0.1.0.yml) contains 11 code objectives associated with MAP 2.3 and 3.5, MEASURE 2.3, 2.4, 2.6, 2.7, 2.11, and 3.1, and MANAGE 2.3, 2.4, 3.2, and 4.1. NIST AI RMF is voluntary, so selecting it produces `recommended-practice` results rather than legal obligations.

Both packs map their objectives to canonical technical controls. For example, EU Article 14's human-review objective and NIST MAP 3.5 can both use the `human-review-gate` control. ComplyScan searches the repository once, gives matching shared evidence the same stable fingerprint, and then explains what that evidence means under each selected source. This avoids duplicating scanners while preserving distinct citations, applicability logic, and legal nature.

Each objective declares its source reference, developer-facing description, applicability note and scope, relevant configured AI activities, external-use condition, eligible file kinds, evidence signals, and required verification. Reconciliation consumes those YAML fields directly; there is no second hard-coded objective mapping in Go. Run `complyscan framework list` to inspect built-ins, select packs during guided setup, or configure them explicitly:

```yaml
frameworks:
  - eu-ai-act-technical-evidence
  - nist-ai-rmf-technical-evidence
```

The packs accept only source, configuration, dependency, test, CI, container, and infrastructure file kinds. They do not contain objectives for policies, risk assessments, training records, conformity documents, attestations, or other evidence intended for a future dashboard. The CLI runs technical checks even when no system profile exists; declared context remains a separate, provisional report section rather than an activation gate. Applicability conditions prioritize and reconcile objectives but never suppress independent repository discovery.

Every technical objective receives one of these statuses:

| Status | Meaning |
| --- | --- |
| `candidate-evidence` | One or more eligible files matched all configured signal groups. The code still requires technical and human verification. |
| `not-detected` | The bounded scan did not locate the configured technical signal. This does not prove the implementation is absent. |
| `not-evaluated` | The technical check could not be evaluated. This status is reserved for explicit limitations rather than treated as a failure. |

Evidence matching requires every configured keyword group and path signal to match one eligible file. A language-neutral repository graph then attaches bounded structural context to each candidate. The current indexers support Go, Python, JavaScript, and TypeScript and record functions, methods, types or classes, imports, calls, routes, configuration access, authorization-like checks, persistence calls, audit/logging calls, and tests. Framework relationships cover common FastAPI, Flask, Django, Express, Fastify, Next.js, and NestJS patterns. The graph classifies anchors as production-reachable, exported entry candidates, test-only, or not reached, and reports unresolved questions such as a missing connected authorization check. Unsupported source is visible as coverage debt and prevents an unmatched source objective from being presented as fully evaluated.

Saved results include a stable fingerprint, repository-relative path, line number, file kind, matched terms, report-safe symbol metadata, imports, relationships, reachability, and unresolved questions—not source excerpts. The technical-evidence schema is versioned independently, and the pack version, SHA-256 content digest, source edition, scan identity, target, and discovery scope remain visible for reproducibility. Go uses the standard-library parser; Python, JavaScript, and TypeScript use dependency-free conservative structural indexers. Dynamic dispatch, runtime route registration, and other source languages remain explicit coverage debt for technical-objective context, while deterministic rules retain their separately documented multi-language coverage.

These technical packs are deliberately not complete regulatory or governance catalogs. The future dashboard will own documentary, organisational, operational, and attestation objectives and combine them with the same canonical control IDs. Authoritative sources are the [official EUR-Lex text](https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32024R1689) and [NIST AI RMF 1.0](https://doi.org/10.6028/NIST.AI.100-1). South Korea's AI Basic Act is not implemented yet; adding it requires a separately reviewed, versioned code-only mapping and must not reuse EU applicability logic.

When Ollama is explicitly enabled, ComplyScan performs a separate technical evidence investigation after deterministic matching. Existing candidates receive bounded graph relationships and connected symbol excerpts. A likely-required objective with no candidate receives a separate extended-search target containing eligible-file coverage, ranked repository excerpts, and a bounded manifest. When a system is declared, both target types are built from only the paths assigned or intentionally shared with that system; the graph, repository digest, manifest, initial excerpts, model-directed follow-up, cache identity, returned observation, and reconciliation attachment retain that system boundary. Before its final decision, the model may request one follow-up round containing at most three literal search terms and repository-relative path hints. Trusted code—not the model—searches only already eligible and system-owned discovered files and returns at most three bounded excerpts. Globs, regular expressions, traversal, absolute paths, commands, filesystem access, and additional rounds are rejected or safely skipped. The model receives no evidence fingerprint and returns no binding identifiers; trusted ComplyScan code attaches the sole decision to the submitted system, objective, and fingerprint. It must separate supporting, contradictory, and missing evidence and may cite only paths included in the submitted context. Trusted code derives the final assurance level from the guarded strength and reachability. Neither the investigation nor its assurance can change objective status, decide legal applicability, establish runtime effectiveness, or affect the exit code.

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

frameworks:
  - eu-ai-act-technical-evidence
  - nist-ai-rmf-technical-evidence

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
    model: qwen3.5:9b
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

ownership:
  - paths: [services/candidate-ranking/**]
    systems: [candidate-ranking]

baseline: .complyscan-baseline.json

suppressions:
  - rule: AI-LOG-001
    path: testdata/**
    reason: Synthetic prompts are intentionally logged in test fixtures.
```

One repository may declare multiple entries under `systems`. Controlled fields reject unsupported or misspelled values, while users and affected groups remain short factual labels. Confirmed profiles require a named reviewer and date. A human `applicable`, `not-applicable`, or `uncertain` decision additionally requires a rationale; the default is `needs-review`.

`ownership` is a repository-layout mapping, not a legal conclusion. Each positive, repository-relative gitignore-style path rule names one or more declared system IDs. One owner means dedicated code; multiple owners in the same rule mean intentionally shared code—for example, `systems: [candidate-ranking, support-assistant]` after both IDs have been declared. Overlapping matching rules with different owner sets are reported as conflicting, and unmatched paths remain unassigned. ComplyScan attaches only assigned, shared, or explicitly disclosed single-system-inferred evidence to a system. Run `complyscan ownership setup` for guided configuration or edit this version-controlled section directly.

The configuration is intended for version control. Do not put secrets, source excerpts, personal records, confidential customer names, or sensitive case details in a profile. Record concise categories and link to access-controlled evidence outside ComplyScan when necessary.

The scanner also respects `.gitignore` and always ignores source-control metadata, common dependency directories, virtual environments, caches, and build output. Binary files, symlinks, and files larger than 1 MiB are not read. Nested Git repositories are skipped unless `--include-nested-repositories` is set.

Discovery is bounded to 25,000 text files and 100 MiB of text by default. Terminal scans report discovery progress every 500 files. Use `--max-files` and `--max-total-bytes` to tune the limits, repeat `--exclude` for temporary exclusions, or use `--tracked-only` to restrict a scan to the Git index. Each option also has a matching key under `scan` in `.complyscan.yml`.

`--changed-since <git-ref>` limits code-level inventory, logging, and secret findings to files changed since that commit or branch. It includes committed changes, staged and unstaged changes, and untracked files. Repository-wide documentation and risk-evidence checks still use the complete discovered repository. This is intentionally a CLI flag rather than persistent configuration because the comparison reference belongs to a particular local or CI run.

Every finding has a stable SHA-256 fingerprint in structured output. Reviewed findings can be suppressed by `rule`, a Git-style `path` pattern, an exact `fingerprint`, or a combination. Every suppression requires a `reason`; suppressed findings are excluded from output and exit-code evaluation, and their count remains visible in the report summary.

For an existing repository, `complyscan baseline .` records the current findings in `.complyscan-baseline.json` without storing source evidence. Commit that deterministic file and future scans will report only findings whose fingerprints are new. Use `--baseline path/to/file` to select another baseline for a scan or `--no-baseline` to inspect every finding.

## Optional model review

The built-in scan default remains deterministic. Interactive `complyscan setup` offers local Ollama first, before repository analysis, and performs each installation step only after confirmation. When the selected model is ready, setup uses one separate bounded structured request to draft repository-evident questionnaire answers; those drafts do not become configuration until reviewed through the prompts. The picker lists models already installed by Ollama, labels `qwen3.5:9b` as the recommended default candidate, retains `qwen3:8b` as the previously validated option, shows a small recommendation set, and accepts any exact Ollama tag. Manual setup remains available:

```bash
ollama serve
ollama pull qwen3.5:9b
complyscan scan . --review ollama --ollama-model qwen3.5:9b
```

You can instead set `ai.provider: ollama` in `.complyscan.yml`. `--review none` disables a configured reviewer for one scan. If explicitly enabled review cannot connect, times out, cannot find the model, or returns invalid structured output, the scan exits with code `2` rather than silently omitting the requested review.

### BYOK remote providers

OpenAI, Anthropic, and Gemini use fixed official HTTPS endpoints and the same bounded prompts, schemas, binding checks, redaction, caching identity, and advisory-only result model as Ollama. The API key value is read from an environment variable at request time; it is never accepted as a configuration field, CLI value, report field, or cache field. Example:

```yaml
ai:
  provider: openai
  remote:
    model: gpt-5.6-terra
    api-key-env: OPENAI_API_KEY
    timeout-seconds: 360
    max-findings: 20
```

```bash
export OPENAI_API_KEY="your-key-from-your-secret-store"
complyscan scan .
```

The guided equivalent is `complyscan setup`. Non-interactive setup must explicitly include `--allow-remote-review`. The current starting choices are `gpt-5.6-terra` for OpenAI, `claude-sonnet-5` for Anthropic, and `gemini-3.6-flash` for Gemini; users may enter another exact model ID supported by their account. These defaults are configuration starting points, not proof of availability or quality. Run `complyscan doctor --probe-review` for a small live structured-output check before scanning sensitive repositories. See the official [OpenAI Responses and model documentation](https://developers.openai.com/api/docs/models), [Anthropic Messages API and authentication documentation](https://platform.claude.com/docs/en/api/messages/create), and [Gemini Interactions structured-output documentation](https://ai.google.dev/gemini-api/docs/structured-output).

Remote review sends bounded secret-redacted finding records and selected source excerpts outside the machine, may incur cost, and is governed by the provider account's processing and retention settings. ComplyScan sets `store: false` for OpenAI and Gemini requests, refuses redirects, and does not expose configurable remote endpoints. Those controls do not replace an organisational review of provider terms, region, retention, proxies, contracts, and permitted source-code processing.

Technical investigations are cached in the operating system's private user-cache directory, not in the scanned repository. The cache stores the same bounded, redacted observation used in reports plus hashes of submitted context; it does not store the submitted source-context records. Model rationales and evidence summaries are instructed not to quote code, but may still describe repository details and should be treated as potentially sensitive data. Reuse requires the same provider, model tag, prompt version, control-pack ID/version/digest, objective, evidence fingerprint, complete bounded input digest, and full discovered-repository digest. Any repository code, context, pack, prompt, provider, or model-name change therefore triggers a new request. Use `--refresh-review` to deliberately bypass existing technical observations.

The experimental default candidate is the official Ollama `qwen3.5:9b` model. The previously validated `qwen3:8b` remains selectable, and the official `qwen3-coder:30b` is available as a substantially larger unvalidated option. The model remains configurable. Setup may install Ollama, start its Homebrew service, or pull the selected model only after explicit confirmation; normal scans never install software or models. Fake-transport tests enforce the structured-output, redaction, injection, system/objective binding, grounded-path, bounded-retrieval, and isolated-test interpretation contracts. The recorded prompt-version 10 fixture gate and timing results were produced with `qwen3:8b`; they do not transfer automatically to `qwen3.5:9b`. Broader representative quality and performance evaluation, plus the maintained applicability reassessment, remain open before Ollama investigation can leave experimental status.

ComplyScan uses two separate review flows, both bounded by `max-findings`, regardless of provider. The finding flow sends visible, unsuppressed deterministic finding records with re-redacted metadata such as the rule ID, fingerprint, title, message, relative path, line, short evidence, remediation, severity, and confidence. The technical flow can make a search-planning request followed by one final decision request per system-specific existing candidate or likely-required missing-evidence investigation target. Candidate targets include reachability, imports, graph relationships, unresolved questions, a bounded match window, and connected symbol excerpts. Extended-search targets include deterministic search terms, coverage counts, up to six ranked excerpts, and at most 200 eligible repository paths from that system's scope. A requested follow-up can add at most three 2,000-character excerpts from the same allowed paths. The evidence fingerprint is withheld. ComplyScan never sends a complete repository or an unbounded file. Input excerpts are re-redacted and are not copied into the saved report. Shared evidence may create a separately bound target for each owning system, so remote investigation may produce multiple billable API calls per objective.

Ollama endpoints must be `localhost` or a loopback IP; ComplyScan bypasses HTTP proxies for that local route. Remote adapters use fixed official HTTPS endpoints, may respect an operator-configured HTTPS proxy, and refuse redirects. Repository strings, code, and comments are explicitly labelled untrusted in the fixed prompts. Finding output must preserve submitted identifiers; unknown, changed, duplicate, or malformed bindings fail that review. Technical output contains only decision and evidence-summary fields, so the model cannot choose which objective receives its response. Any cited path outside the submitted bounded context fails the requested investigation.

Finding observations are attached to existing fingerprints as `confirmed`, `uncertain`, or `not_supported`. Technical investigations remain attached to the exact system, framework, objective, and evidence target. Candidate validation and missing-evidence search use only code assigned or shared with that system; conflicting and unassigned paths are excluded. Both review types are advisory: they cannot create or remove findings, change objective status or severity, update baselines or suppressions, or affect the failure threshold. Terminal, Markdown, and schema-version 5 JSON expose the framework, system ID, ownership mode, and repository-file count for every scoped investigation; SARIF carries finding review only. ComplyScan requests non-streaming schema-constrained output; Ollama additionally receives temperature zero, while modern remote models use their provider defaults where sampling parameters are unsupported or deprecated. Model output can still vary.

Deterministic, transparent guardrails constrain two recurring reasoning failures: discussion-only blog, documentation, FAQ, example, and quiz components cannot become implementation evidence, while executable graders, rubric renderers, assertions, and evaluation templates remain reviewable evidence even when static analysis cannot resolve dynamic registration. The report preserves the model's original strength in `model_strength` whenever a guardrail changes it.

The loopback restriction controls where ComplyScan sends review records. Ollama itself may download models or route specially configured cloud models; select and operate a genuinely local model when a no-cloud boundary is required.

## Optional isolated execution verification

ComplyScan can run user-selected test commands in preloaded local Docker or Podman images and attach their bounded, redacted results to terminal, Markdown, and JSON reports. Reusable recipes live in `.complyscan.yml`, but configuration alone never executes them: every scan must opt in with `--verify`. The repository is mounted read-only; each container has no network, no added capabilities, a read-only root filesystem, bounded CPU, memory, processes, temporary storage, output, and time; and commands run directly without a shell. ComplyScan never pulls an image. The image itself remains a user-selected trust boundary.

The recommended setup is interactive:

```bash
complyscan verify setup
```

The wizard uses the existing system profile to list only likely-required technical objectives for one selected system. It explains each objective in developer language, shows current repository signals, detects common native test commands (`go test`, pytest, npm/pnpm/yarn test scripts, and Make test targets), and asks which numbered objectives the exact command genuinely exercises. It then asks for the container runtime, preloaded local image, stable recipe ID, and timeout before showing a final summary. Detection is a suggestion, never proof: the user must confirm the executable, arguments, objective mapping, system ownership, and final save. The wizard never runs a test, builds an image, pulls software, or enables future automatic execution. If an argument itself contains a comma, save the recipe and edit its YAML argument list directly.

For scripting or manual review, the equivalent configuration is:

```yaml
verification:
  recipes:
    - id: robustness-tests
      runtime: docker
      image: your-preloaded-project-test-image:local
      command: go
      args: [test, ./...]
      objectives: [eu-aia-15-robustness-failure-handling]
      systems: [candidate-ranking]
      timeout-seconds: 300
```

```bash
complyscan scan . --verify
```

When exactly one system is configured, `systems` may be omitted and is inferred. Repositories with multiple systems must name the systems covered by each recipe so ComplyScan does not guess evidence ownership. Objective and system association remain explicitly user-declared. A passing recipe adds a separate `test-evidence-observed` assurance to the matching reconciliation result. When model review is enabled, each system-scoped objective investigation receives only bounded redacted execution summaries whose recipe declares that same objective and system. It still cannot turn a passing test into a production-effectiveness or compliance conclusion.

`complyscan verify setup` requires an existing system profile because applicability and evidence ownership cannot be inferred safely without it. When exactly one system is configured, the wizard selects it automatically; with multiple systems, it explains why the user must choose one and creates a system-scoped recipe. Run the wizard again for a different system or test command. An existing recipe ID is replaced only after a separate confirmation that defaults to no.

The existing ad-hoc `--verify-image`, `--verify-command`, repeatable `--verify-arg`, `--verify-objective`, and `--verify-system` flags remain available for a one-off run and cannot be mixed with `--verify`. Use `--verify-runtime podman` when preferred and set `--verify-timeout` up to 30 minutes. A test failure is recorded without changing the finding-threshold exit code; container, runtime, or configuration failures exit with code `2`.

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
  - uses: ComplyScan/ComplyScan@v0.1.5
    with:
      severity: medium
      changed-since: ${{ github.event.pull_request.base.sha }}
```

The `changed-since` input is optional. For pull requests, pass the base commit as shown above and use `fetch-depth: 0` so Git can resolve the comparison and merge base. Omit it for a full scan, such as on a scheduled run or push to the default branch.

By default the action fails after uploading when findings meet `fail-on`. Set `fail-on-findings: false` to publish alerts without failing the job, or `upload-results: false` when code-scanning upload is not available.

The action also accepts `review`, `ollama-model`, `ollama-endpoint`, `model`, and `api-key-env`. Ollama must already be installed, running, and loaded with the selected model inside the job. For BYOK review, place the credential value in a GitHub Actions secret exposed under the configured environment-variable name—never in an action input or committed YAML.

```yaml
- uses: ComplyScan/ComplyScan@<release-tag>
  env:
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
  with:
    review: openai
    model: gpt-5.6-terra
    api-key-env: OPENAI_API_KEY
```

## Privacy and security guarantees

In the default deterministic mode, the ComplyScan CLI:

- runs entirely on the local machine;
- makes no network requests;
- collects no telemetry;
- uploads no source code;
- does not require an API key; and
- never prints a complete detected secret.

`complyscan setup` is a separate provisioning boundary. After explicit confirmations it may invoke Homebrew, download and run Ollama's official Linux installer, start the Homebrew Ollama service, and use `ollama pull` to download the selected model. If model-assisted onboarding is chosen and ready, setup makes a bounded profile-draft request after the local or remote privacy boundary has been selected; human answers and the resulting system profile are not submitted. Non-interactive setup performs no installation or model download unless the corresponding install or pull flag is supplied.

Permission errors are reported as warnings where scanning can safely continue. Source excerpts are short and pass through credential redaction before appearing as evidence.

When model review is explicitly enabled, ComplyScan sends only the bounded profile-draft, finding, and technical-context records described above. Profile drafting selects at most 24 relevant README, manifest, configuration, governance, deployment, or AI-linked source contexts and limits each to 2,500 characters after secret redaction. Ollama requests use a validated loopback endpoint; BYOK requests use the fixed official endpoint after setup disclosure and consent. Submitted source-context records are not stored in the evidence bundle or OS user-cache. Model rationales, questions, and suggested actions may describe repository details; they are re-redacted and length-limited before reporting or caching. Human answers and system profiles are not sent to any reviewer. Generated reports and cached observations remain local unless a future dashboard connection is explicitly enabled.

The optional GitHub Action uploads SARIF metadata to GitHub code scanning when `upload-results` is enabled. That SARIF contains finding messages, repository-relative paths, line numbers, and fingerprints, but not source excerpts or detected credentials. When a job explicitly enables any model reviewer, SARIF also contains the advisory verdict, confidence, rationale, suggested action, provider, and model for reviewed findings.

## Development

```bash
go mod download
go test ./...
go vet ./...
go build ./cmd/complyscan
```

The pipeline has two independent inputs: declared system configuration determines which technical objectives are likely relevant, while repository discovery builds the AI inventory and technical evidence without trusting that configuration. Explicit path ownership connects discovered evidence to the correct declared system. A deterministic reconciliation layer combines the streams, preserves mismatches and uncertainty, and leaves unmatched or conflicting evidence unresolved instead of guessing. Optional model review annotates existing owned evidence afterward and cannot alter applicability, deterministic evidence, or reconciliation status. See [the architecture notes](docs/architecture.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

The labelled detector corpus lives under `testdata/evaluation`. Its test reports precision, recall, true positives, false positives, false negatives, and negative cases; the build fails if precision drops below 95% or recall below 90%.

The separate technical-evidence benchmarks live under `testdata/technical-evaluation`. Source-specific EU and NIST manifests label complete objective candidates plus their expected anchor, production/test reachability, required or forbidden graph relationships, and indexed language. Run `./scripts/evaluate-technical-evidence.sh` for EU and add `--manifest testdata/technical-evaluation/nist-manifest.json` for NIST; `--format json` produces a machine-readable result. CI enforces both versioned synthetic thresholds. A manual `./scripts/evaluate-external-repositories.sh` study checks source-free EU labels against ten pinned permissively licensed public repositories without committing third-party code; add `--manifest testdata/technical-evaluation/external/nist-manifest.json` for the independently reviewed NIST labels. Add `--review ollama` for the slower semantic gate, which now defaults to `qwen3.5:9b`; dated benchmark results remain explicitly attributed to `qwen3:8b`. These are regression gates and tuning evidence, not claims of general real-world accuracy. See [the benchmark guide](docs/technical-evidence-benchmark.md).

AI-assisted onboarding has a separate labelled quality gate under `testdata/profile-draft-evaluation`. Run `./scripts/validate-profile-draft.sh` to test the current Ollama candidate against expected setup facts, a documentation-only hard negative, forbidden inferences, grounded citations, and explicit precision/recall/time thresholds. The full four-case run may take up to 20 minutes; `COMPLYSCAN_PROFILE_DRAFT_CASES="documentation-hard-negative"` selects a quick single case. See [profile-draft validation](docs/profile-draft-validation.md).

ComplyScan applies the same evidence discipline to itself. Its maintained [AI applicability assessment](docs/AI_SYSTEM.md) distinguishes the default deterministic mode from local and remote inference-enabled configurations and records the required pre-release reassessment. The companion [technical risk assessment](docs/risk-assessment.md) records foreseeable harms, controls, residual risks, and review expectations. These documents support governance; they are not self-certification.

## Roadmap

Future releases may add:

- resumable onboarding drafts for repositories where model-assisted setup is interrupted before human confirmation;
- broader labelled coverage for more languages, frameworks, model gateways, and data flows;
- model and AI dependency supply-chain inventory;
- a reviewed South Korean AI Basic Act code-only pack, with jurisdiction-specific applicability kept outside the shared scanner controls;
- richer dashboard synchronization and review workflows for system-scoped, multi-framework evidence bundles;
- a dashboard catalog that combines CLI code evidence with uploaded documents, declarations, attestations, and operational evidence;
- automatic opt-in synchronization of the exact local JSON evidence bundle;
- graph indexers for additional source languages and framework-specific route/call resolution;
- iterative, still-bounded local context retrieval for unresolved technical questions;
- richer, rule-specific local review prompts and measured live-model evaluation corpora;
- optional ComplyScan Cloud integrations.

Ollama, OpenAI, Anthropic, and Gemini are implemented review providers. The built-in scan default remains `none`; interactive setup presents Ollama first because it can keep bounded context on the local machine. Every remote provider requires an explicit external-processing confirmation, an environment-only API key, and a user-selected model before any profile-draft context is submitted.

## Disclaimer

ComplyScan findings are automated technical observations. They are not legal advice, a legal determination, a conformity assessment, or compliance certification. Regulatory obligations depend on the complete AI system, its intended purpose, deployment context, actors, data, and real-world use. Obtain qualified legal and compliance review for decisions about the EU AI Act or other laws.

Licensed under the [Apache License 2.0](LICENSE).
