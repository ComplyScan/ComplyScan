# ComplyScan

> ComplyScan is a developer-first scanner that discovers AI implementations, checks code-level safeguards, and detects governance regressions before code reaches production.

ComplyScan is an open-source, offline-capable CLI for finding technical signals that deserve review against selected AI governance sources. Ordinary guided setup asks only for the optional model/provider and technical evidence packs; the scan infers code-visible facts, while organisation-only unknowns stay in the report for a future compliance-owner workflow. A framework-neutral control layer maps code and configuration signals to both EU AI Act technical objectives and voluntary NIST AI RMF practices. `complyscan scan` is the single normal workflow: it always runs the local deterministic checks and, when repository intent is backed by matching private trust recorded by guided setup on this machine, follows them with bounded advisory AI reasoning. Local structural analysis selects best-effort-redacted candidate excerpts for the configured OpenAI, Anthropic, Gemini, or experimental Ollama provider; small queues fit one package, while larger queues use multiple bounded source requests plus synthesis. Use `complyscan scan --deterministic-only` to guarantee that no model or provider credential is used.

ComplyScan does **not** interpret a complete system, determine an EU AI Act classification, certify compliance, or replace legal and compliance professionals. A finding is a review prompt—not a claim that a system violates the law.

## Install

> Current release: v0.1.7. This README follows unreleased development on `main`, including the unified scan workflow. For behavior available in v0.1.7, use the documentation attached to that release.

macOS and Linux users can install ComplyScan and immediately start guided setup with one command:

```bash
curl -fsSL https://complyscan.github.io/ComplyScan/install.sh | sh
```

The installer is published from this repository through GitHub Pages. It detects the operating system and CPU architecture, downloads the matching release archive, verifies its published SHA-256 checksum, installs `complyscan` into `~/.local/bin`, and launches `complyscan setup` through the terminal. It does not use `sudo`. Pass `--no-setup` for automation or pin both the installer and binary version:

```bash
curl -fsSL https://github.com/ComplyScan/ComplyScan/releases/download/v0.1.7/install.sh | sh -s -- --version v0.1.7 --no-setup
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
go build -ldflags "-X main.version=0.1.7 -X main.commit=$(git rev-parse --short HEAD) -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o complyscan ./cmd/complyscan
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
complyscan scan . --verbose
complyscan scan . --format json
complyscan scan . --format sarif > complyscan.sarif
complyscan scan . --severity high --no-color
complyscan scan . --tracked-only
complyscan scan . --exclude fixtures --max-files 10000
complyscan scan . --changed-since main
complyscan scan . --deterministic-only # guarantee no model or credential use
complyscan scan . --provider openai --model gpt-5.6-sol --api-key-env OPENAI_API_KEY # one-run override
complyscan ai-uses show # inspect the human-owned AI-use register
complyscan ai-uses setup # optionally improve precision from the latest AI suggestions
complyscan ai-uses edit # update an existing saved use without another review
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

Running `complyscan` is the recommended first command. With no configuration it launches `complyscan setup`, a four-step flow: inspect the repository and create a report target locally; choose the optional AI provider and model; select the technical mappings, such as the EU AI Act or NIST AI RMF; then review, save, and optionally run ComplyScan. Ordinary setup does not ask developers to classify organisation roles, decision impact, human oversight, data categories, deployment, or legal applicability. The scan records positive code-visible facts as evidence-backed report observations. Organisation, market, contractual, operational, and legal facts that repository code cannot establish remain explicit unknowns in the report and never block setup or the code scan. Selecting the EU technical pack means “map this code against these technical objectives,” not “the EU AI Act legally applies.” A compliance owner can optionally use `complyscan setup --advanced` or `complyscan profile setup --replace` for a reviewed detailed profile; this is the current CLI precursor to a future dashboard workflow. Terminal setup saves private recovery checkpoints between major stages and offers to resume them after interruption; these drafts contain configuration choices but no repository source or API-key value and are removed after `.complyscan.yml` is saved.

In an interactive terminal, fixed single-answer questions use an arrow-controlled radio menu, multiple-answer questions use checkboxes, and confirmations show a highlighted Yes/No choice. Move with ↑/↓, tick or untick with Space where applicable, press Enter to confirm, and press ← to revisit the preceding question without losing completed answers. Text and accessible prompts accept `back` because they do not receive interactive arrow-key events. `Ctrl+C` still cancels setup. The standard cloud picker contains only ComplyScan's provider-specific shortlist; experimental local and explicitly configured compatible providers retain exact-model entry. These controls also cover the system, test-command, and objective choices in verification setup. Redirected setup scripts keep the stable numbered or text interface. Set `COMPLYSCAN_ACCESSIBLE=1` to force that interface for screen readers or terminals where cursor-based controls are unsuitable.

Ordinary EU setup deliberately leaves organisation and applicability context unresolved. The report's applicability-readiness view uses `incomplete` when facts needed by a mapping remain unknown, `factually-ready` when they are present without a named reviewer, and `human-reviewed` when an accountable reviewer has confirmed them. These statuses describe input quality only. They do not decide that the EU AI Act applies. Unknown context never prevents repository discovery or technical-objective scanning; it keeps legal and organisation-dependent mapping provisional until a compliance owner optionally completes the advanced profile.

First-run setup finishes by offering to run ComplyScan or save configuration only. The scan always runs deterministic checks. If the user selected an AI provider after the privacy and cost disclosure, setup saves repository intent as `ai.review-on-scan: true` and a matching private machine-local trust record; the same-machine scan can then run the configured advisory review. If no provider was selected, the scan remains model-free. Remote setup shows only shortlisted model IDs available to the account and saves only the API-key environment-variable name in repository configuration.

`doctor` checks the installed build, repository configuration, Git detection, report-directory permissions, local Ollama readiness, the presence—not the value—of a configured remote credential, and any cached model-qualification status. Missing reviewer dependencies are warnings in an ordinary check and do not block the deterministic part of a scan. `complyscan doctor --probe-review` explicitly requests fresh synthetic finding and repository-shaped structured-output checks, so a missing credential, service, or model becomes a blocking failure and the compatibility requests are skipped. Remote probes may incur a small provider charge and never contain repository data.

Maintainers can run both hosted quality gates with `./scripts/validate-cloud-model.sh PROVIDER MODEL API_KEY_ENV`. Local historical evaluation remains available through `./scripts/validate-ollama.sh` and `./scripts/validate-profile-draft.sh`. See the [model support policy](docs/model-support-policy.md) and [Ollama live-model validation](docs/ollama-validation.md).

Every ready model receives two source-free synthetic compatibility checks before repository context is sent: one finding-record binding and one minimal repository-shaped contract. They normally use two requests and share a four-request maximum including typed temporary provider/transport retries inside the existing qualification timeout; permanent quota, oversized input, cancellation, and contract-invalid output stop promptly, and request/token accounting remains visible. Successful qualification is cached for 30 days and invalidated by provider, exact model or available Ollama digest, endpoint, prompt-contract changes, or qualification contract changes. The identity includes qualification contract version 3, repository-analysis prompt version 17, setup-draft prompt version 6, and the finding and technical-review prompt contracts. Qualification cache schema 5 is stored as `model-qualification-v5.json`, so an older candidate-ID repository contract cannot authorize source transfer. Compatibility does not establish a maintained quality claim. The standard picker separately reports whether the exact model passed profile-assistance and technical-review gates; every current cloud candidate remains labelled pending until those paid live gates are run and reviewed. Automation never installs software or downloads a model unless `--install-ollama` or `--pull-model` is explicitly passed. Non-interactive remote setup additionally requires `--allow-remote-review`, and contacts the model only when `--qualify-model` is supplied; use `--non-interactive --review none` for a network-free starter configuration. See [automatic model qualification](docs/model-qualification.md).

`scan` defaults to the current directory, so `complyscan scan` and `complyscan scan .` are equivalent. It always runs the deterministic foundation and prints findings as they are discovered. Automatic AI review requires both `ai.review-on-scan: true` and a matching private trust record created by guided setup on this machine. The trust identity binds the provider, endpoint, model, and credential environment-variable name, so cloning a repository or changing that identity cannot silently authorize source transfer. `--provider` and the related provider/model options are explicit one-run consent. A missing trust record, key, model, or provider leaves an honest deterministic report and marks or explains the AI layer as unavailable. `--require-ai-review` is a completeness policy, not consent: by itself it never activates a model and returns exit code `2` after the deterministic report when trust is absent. Use `--deterministic-only` when the run must not read a provider credential or contact a model. Legacy configurations that omit or disable `ai.review-on-scan` retain their old deterministic scan boundary and print migration guidance. The default completion is concise, and `--verbose` prints full framework and review detail. Use `unknown` rather than guessing.

The trust record lives in the operating system's private user-cache directory, outside the repository. It stores canonical repository/configuration paths, a settings digest, and a timestamp—not source or an API-key value. Its identity also covers context strategy, token budget, timeout, and finding limit. Run setup again with model-free analysis to revoke automatic review for that repository and configuration.

`profile show` reports conservative EU AI Act scope, high-risk screening, technical-mapping readiness, and unresolved facts from the declared profile. `profile setup` adds a system to an existing config; use `--replace` to update a profile with the same ID. When a repository declares multiple systems, setup offers a separate path-ownership wizard. `ownership setup` can replace those mappings later without repeating the applicability questionnaire, and `ownership show` prints them as terminal text or JSON. Automated screening, factual readiness, human decisions, and missing context remain separate. ComplyScan never converts a scope signal into a compliance certificate.

`inventory` produces a component-focused view rather than compliance findings. It aggregates detected providers and frameworks with their technical evidence, runtime/test/configuration scope, package versions, confidence, and source locations. Its JSON output has a versioned schema for downstream tooling.

### Stable AI-use register

The deterministic component inventory answers “which technical AI signals are present?” A configured scan may separately suggest product-level AI uses supported by code citations. Neither result silently becomes durable project state. The report is useful without another setup step. Developers who want more precise per-use mappings can optionally run `complyscan ai-uses setup` to confirm a new use, merge a suggestion into an existing use, dismiss the unchanged suggestion with a reason, or decide later. That guided command lets the developer set the name, description, repository paths, associated configured systems, and reviewer attribution. Run `complyscan ai-uses edit` to revise an existing saved grouping later without scanning the repository or contacting a model. Editing preserves the stable ID and suggestion links while allowing the developer to change its descriptive fields, scope, associations, active or retired lifecycle, and draft or confirmed review attribution.

Confirmed groupings are stored in the human-owned, version-1 `.complyscan/ai-uses.yml` file, which is designed to be reviewed and committed with the repository. Stable IDs are generated locally; model-authored AI-use IDs are never persisted as project identity. An exact reviewed-suggestion fingerprint records explicit links without treating shared repository paths as proof that two product uses are the same. The fingerprint deliberately favours safety over silent merging, so materially reworded or relocated model suggestions may be presented again for a developer decision. `complyscan ai-uses show` displays the saved register without running a scan.

Every scan loads the register locally, overlays current technical signals and any model suggestions in the report, and never mutates the file. For every active developer-confirmed use, ComplyScan filters technical-objective evidence to the use's saved paths and evaluates those objectives separately under each associated configured system. The system supplies declared applicability facts; the AI-use register supplies code scope. When a matching signal exists only outside the saved paths, the result records that separately so a developer can identify shared code or correct the scope instead of treating it as a simple absence. A missing or stale system association leaves legal and other context-dependent applicability unresolved; framework-wide voluntary practices can still remain recommendations. One use associated with several systems receives a separate result for each system context. Draft and retired uses remain visible but do not create current requirement mappings.

The same report overlay now adds positive repository facts to each exact saved or model-suggested AI-use ID without turning them into questionnaire answers. Local deterministic analysis records scoped runtime model-provider references, but an SDK import, endpoint string, or environment-variable name alone is not promoted to `inference`. When the optional repository model pass completes, it may add typed activity and other code facts only with checked citations inside that use's submitted files. A scoped executable or installer becomes a `local-cli` fact only after positive AI activity is supported. Absence never becomes `no`, `none`, `production`, or another negative or operational claim. Facts remain scan-owned observations: they do not update `.complyscan.yml`, `.complyscan/ai-uses.yml`, applicability, requirement mappings, or CI gates.

Repository artifacts can also support conditional `possible` role candidates for provider, downstream provider, or deployer. These are explanations of what the code could indicate, not statutory classifications. Contracts, actual professional use, operating regions, the organisation controlling the product, actual placing on the market, and the name or trademark used cannot be established from repository code. Those organisation-only unknowns appear in the report for a future compliance owner; they never create a developer setup task or a CI block.

Use-scoped mappings are advisory and do not change deterministic findings or CI thresholds. When the configured AI layer runs, ComplyScan derives a typed context for each active confirmed use that has files inside the current model boundary. It contains the stable human-owned ID, description and path scope, associated configured systems, the exact files selected for that use, and only objectives currently screened as likely required or recommended. The raw register is not sent. The model evaluates these use/objective/system combinations separately inside every applicable bounded source request. ComplyScan then merges and preserves those validated direct observations locally; the grouping-only synthesis request does not repeat or alter them. Definite decisions require checked citations from that use's submitted files; an uncited result can only say that the bounded package is insufficient to decide. Trusted validation rejects omitted or unknown combinations and out-of-scope citations.

A validated direct observation can therefore be attached to the named use even when two uses intentionally share code; the stable ID expresses which supplied use/objective/system combination was evaluated. A directly bound uncertain result may be retained without citations only as “cannot determine.” A generic observation without an AI-use ID still attaches only when its checked citations identify exactly one confirmed use. Uncited repository-wide conclusions, out-of-scope citations, and generic observations ambiguous across overlapping scopes remain repository-level. In every case the register stays human-owned: model output can neither rename, merge, create, retire, nor otherwise edit a saved use. If two uses need different questionnaire answers, associate them with different configured system profiles; sharing one system deliberately shares that declared context.

The exact manifest is excluded from repository discovery and model evidence, just like the active ComplyScan configuration. During `--changed-since` runs, deterministic signals and deterministic per-use repository facts are still collected repository-wide, but only changed and connected files enter the model review. Model-reasoned facts therefore carry `changed-plus-connected` coverage while local mechanical facts carry `full-repository` coverage. Only the intersection of a saved use's paths and that changed-plus-connected boundary is submitted; a use with no selected file receives no use-specific model context. A saved use outside that scope may therefore show a current local signal while remaining unreviewed by the model, and absence from the change can never delete or retire a human-owned entry. The private repository-review cache binds the active repository scope and derived confirmed-use IDs, path scopes, and objective/system combinations; the deterministic selector derives the submitted-file set from those inputs. Changing any bound input invalidates reuse. Confirming a repository grouping records developer knowledge, not legal applicability, safeguard effectiveness, or compliance.

The two `generate` commands use that inventory to create `docs/AI_SYSTEM.md` and `docs/risk-assessment.md`. The generated files are deliberately marked as drafts, preserve discovery warnings, and contain explicit human-review fields. Existing documents are protected unless `--force` is supplied.

Terminal scans print findings as rules discover them and finish with a concise inventory, technical-objective, and requirement-mapping summary. Full framework detail remains in the JSON evidence bundle or can be printed with `--verbose`. EU reconciliation distinguishes likely requirements, gaps, mismatches, and unresolved applicability. NIST reconciliation labels selected practices as recommendations, never legal requirements. Repository evidence that cannot safely be assigned to a configured system remains unresolved. Explicit reviews add advisory safeguard decisions. “Without detected evidence” and “not found after investigation” remain bounded search statements, never proof of absence or breach.

Every completed scan publishes an immutable Markdown/JSON pair under `.complyscan/reports/history/<UTC date and time>/report.{md,json}`, then refreshes `.complyscan/reports/latest.md` and `latest.json`. History directories use a readable UTC name and are never overwritten with different content. The developer-facing Markdown starts with separate scan-completion, technical-evidence, and legal-applicability statuses, followed by at most three prioritized actions. It distinguishes inferred AI workflows from supporting infrastructure and evaluation tooling, keeps code-derived details collapsible, and leaves exhaustive evidence in JSON. The model supplies structured, citation-checked technical observations; deterministic local code decides the displayed counts, priorities, labels, and Markdown layout. Dashboards and other integrations must consume `latest.json`, never parse the presentation-oriented Markdown. Deterministic matches remain code signals. When the configured AI layer completes, it can add one of four advisory repository verdicts: implemented, partially implemented, not implemented in the reviewed code, or undetermined. These verdicts do not decide legal applicability, production enablement or operational effectiveness, and they do not change the deterministic CI threshold. Scans save a complete preliminary report before their first model request and checkpoint completed layers, so provider failures cannot erase local results. Use `--require-ai-review` when automation should return exit code 2 unless a separately trusted or explicitly requested AI layer completes; the flag itself never authorizes processing. Use `--no-report` to disable persistence or `--report-dir` to select another directory inside the target.

JSON and SARIF 2.1.0 output remain buffered so they are valid and deterministically ordered. Schema-version 14 JSON stdout and `latest.json` include applicability, the raw technical AI inventory, the `ai_use_inventory` overlay, per-use `ai_use_mappings`, technical evidence, reconciliation, the explicit `repository_analysis_run` lifecycle, exact `member_observation_ids` for every inferred repository AI use, checked cross-batch `resolved_evidence_gaps`, and the repository review's `provider_requests`, `source_batches_started`, `source_batches_completed`, and `source_batches_total` counters. Version 14 distinguishes batch-local gaps answered by another validated member from globally unresolved technical questions and report-only organisation unknowns. `source_batches_completed` counts batches whose model output passed trusted validation; `source_batches_started` separately records source-bearing batches actually attempted. Version 13 added request and started-batch accounting, version 12 introduced exact observation membership, version 11 introduced completed/total batch coverage, and version 10 introduced `repository_facts`, `role_candidates`, and report-level organisation unknowns. Version 9's optional direct confirmed-use observation binding remains available. SARIF includes source locations and stable partial fingerprints for code-scanning integrations, but omits technical-objective summaries that have no single code-scanning location.

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
  Latest human-readable: .complyscan/reports/latest.md
  Latest evidence bundle: .complyscan/reports/latest.json
  Historical human-readable: .complyscan/reports/history/2026-08-14_14-25-30Z/report.md
  Historical evidence bundle: .complyscan/reports/history/2026-08-14_14-25-30Z/report.json
```

## Rules

| Rule | Severity | Purpose |
| --- | --- | --- |
| `AI-DISC-001` | Info | Inventory technically evidenced AI providers and frameworks, including OpenAI, Anthropic, Gemini, Mistral, Cohere, Hugging Face, Ollama, LiteLLM, LangChain, LlamaIndex, Vercel AI SDK, and OpenRouter. Results are aggregated by component with representative locations. |
| `AI-LOG-001` | High | Flag conservative cases where prompt-like or response-like values appear in common Go, Python, JavaScript, or TypeScript logging calls. |
| `AI-SEC-001` | High | Detect likely hardcoded AI API credentials and report only redacted evidence. |
| `AI-DOC-001` | Medium | When AI usage is detected, check for a model card or AI-system documentation. |
| `AI-RISK-001` | Medium | When AI usage is detected, check for repository-level AI risk-classification evidence. |
| `AI-CTRL-001` | High | For a human-reviewed system profile, flag a likely-required technical objective when no configured implementation evidence is detected. |

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

When `ai.review-on-scan` is backed by matching private machine trust, or a provider is supplied with explicit one-run options, `complyscan scan` runs targeted repository AI reasoning after deterministic discovery, secret redaction, file classification, inventory, technical-evidence matching, and graph construction. Local code ranks AI signals, objective matches, production entry points, imports, callers, and nearby graph relationships, then prepares one bounded excerpt for every structurally selected candidate file. A small queue can fit one modest request; a medium hosted queue uses a balanced 48,000-token target and normally produces about two source requests, followed by one grouping synthesis. The target can grow to the 64,000-token latency ceiling and remains bounded by the confidently known model context window, the configured input ceiling, selected evidence size, and live token capacity. The packer balances the complete candidate queue before transfer so an almost-full first bundle and tiny remainder do not become another adaptive split. Providers do not expose a universal context-limit header, so unknown models and compatible gateways retain a conservative 32,000-token ceiling rather than inheriting another provider's advertised limit. When several requests are needed, resolved calls, imports, and declared confirmed-use scope preferentially keep related files in the same bundle when they fit. These are context bundles, not deterministic AI-use classifications; the model still decides semantic membership. Source responses are tightly bounded facts, decisions, and checked citations rather than repeated mini-reports. Global synthesis receives only a compact grouping view; ComplyScan locally reattaches the complete already-validated evidence. A single-package review may request one bounded follow-up with at most three literal searches. `--deterministic-only` never enters this path.

Provider responses may expose effective request-per-minute and token-per-minute capacity through rate-limit headers; there is no portable preflight endpoint that reports an API key's current tier. ComplyScan uses both request and token dimensions when both are available. A live compatibility check can seed the first source wave. When compatibility is reused from the private cache, ComplyScan does not repeat the two synthetic compatibility contracts merely to discover capacity. The first useful source request calibrates the run instead. Complete capacity starts a header-bounded wave. A partial snapshot stays sequential. With no usable capacity metadata, hosted scheduling starts with one request and ramps conservatively after successful waves, up to the local concurrency ceiling of 32. Ollama stays serial because its local server does not advertise portable request/token capacity. Independent hosted source batches run concurrently; output-exhausted members of one completed source wave also recover concurrently when current capacity permits. After all source batches validate, independent groups within each synthesis level use the same scheduler; synthesis levels themselves remain ordered by their dependencies.

Schema-validation failures receive at most two feedback-guided repair calls without weakening trusted local validation. Temporary transport, provider, and rate-limit failures use provider-neutral, jittered retries shared through the same capacity gate, with at most eight retry cycles and ten minutes of cumulative automatic waiting for one logical request. A permanent quota failure returns promptly. Repository analysis has a separate 256-provider-request safety ceiling covering probes, source and synthesis attempts, repairs, and retries. These bounds preserve the deterministic report, but they cannot guarantee a final AI result when a provider permanently rejects the request or cannot satisfy the structured schema. See the [OpenAI API rate-limit response headers](https://developers.openai.com/api/reference/overview#backwards-compatibility).

This adaptive request pattern is bound into the private consent fingerprint as context contract `adaptive-provider-pipeline-v5`. Machine approval created for an older transfer contract does not authorize it automatically; rerun `complyscan setup` to review the multiple-request and variable-cost disclosure, or use explicit one-run provider flags for that invocation.

The active ComplyScan configuration—normally `.complyscan.yml`, or the exact file selected with `--config`—is loaded locally and excluded from discovery and every model context. Only separately constructed typed system facts needed for analysis may be supplied. Other YAML files, including `.github/workflows/*.yml`, retain their normal repository-evidence treatment because they may implement safeguards. Generated output, dependencies, binaries, ignored paths, symlinks, oversized files, and generic unclassified text are not sent. Reports record `targeted-evidence`, selected coverage, token usage, verified citations, and whether the bounded follow-up ran. The explicit `deep`, `full`, and `hierarchical` modes retain broad transfer and adaptive subsystem synthesis for users who deliberately choose that cost and privacy boundary.

See [repository AI code analysis](docs/repository-analysis.md) for the exact modes, selection strategy, transfer boundary, validation contract, and limitations.

The targeted result performs both directions of the product workflow: it discovers technically evidenced AI uses in the structural candidate queue and maps evidence to supplied code objectives. AI activity that cannot be mapped remains visible with a reason. Every positive claim must cite a discovered path and valid line; invented paths, out-of-range lines, unknown objectives, unknown configured systems, invalid classifications, duplicate use IDs, and malformed output reject that response. Rejected raw output is discarded. ComplyScan may regenerate from the same evidence at most twice with sanitized validation feedback, then split source or synthesis work into smaller units where possible; it never weakens citation or binding validation to accept an answer. A completed targeted review has processed every selected candidate-file excerpt before it may report zero model-identified AI uses. Files that never became structural candidates remain explicit coverage debt and are never treated as evidence of absence. The current queue unit is a candidate file excerpt, not an independently discovered invocation path. The model may report only bounded positive technical facts supported by the submitted code; it cannot decide legal applicability, an organisation's statutory role, actual deployment, real human practice, production effectiveness, or compliance.

`bounded-only` retains the previous per-objective technical evidence investigation as a fallback and comparison mode. It can issue a planning and final decision request for each selected candidate or likely-required missing-evidence target, so it may create more calls than the targeted default. Each operation uses at most four total attempts and ten cumulative wait minutes: typed temporary provider, transport, and rate-limit failures retry; truncated decision output retries with a larger bounded allowance; and invalid structured output receives a complete replacement attempt without weakening local validation. Metered failed attempts remain in usage and request accounting. Permanent quota, intrinsically oversized input, cancellation, or exhausted recovery stops only that target. Validated observations for other targets remain in the report. Neither mode's conclusions can change deterministic objective status, decide legal applicability, establish runtime effectiveness, or affect the finding-threshold exit code. Separately, `--require-ai-review` returns exit code `2` when the configured or one-run AI layer is incomplete, including when even one requested technical target was not reviewed.

Investigation conclusions distinguish `technically-substantiated`, `partially-substantiated`, `test-only-evidence`, `unreachable-evidence`, `not-substantiated`, `not-found-after-investigation`, and `cannot-determine`. Assurance levels distinguish a detected signal, AI-substantiated evidence, structurally verified repository evidence, observed test evidence, an extended investigation with no evidence, and an unresolved investigation. Operational proof and legal acceptance are intentionally outside these levels.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Scan completed and no finding met the configured failure threshold. |
| `1` | One or more findings met the configured failure threshold. |
| `2` | The target could not be scanned, configuration/CLI input was invalid, or an explicitly required AI review was incomplete. |

The default failure threshold is `high`. `--severity` filters report output; it does not change `fail-on`.

Most technical-objective results remain advisory and do not change the process exit code. One conservative policy bridge is enforced: `AI-CTRL-001` is emitted only when a named human has reviewed the factual system profile, reconciliation maps an EU technical objective as likely required, and the bounded scan detects no configured implementation evidence. Draft profiles, unresolved applicability, voluntary NIST recommendations, candidate evidence, and every model observation remain non-blocking. This is a technical gap—not a legal breach determination. `framework assess` remains an evidence-only command and exits `0` after a valid assessment even when evidence is not detected.

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
  AI-CTRL-001:
    enabled: true

ai:
  provider: none
  review-on-scan: false # model-free; true is repository intent, and automatic use also requires private local trust
  repository-analysis:
    mode: auto
    # max-input-tokens: 180000  # optional; zero/omitted uses a provider-specific default
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

When a configured AI layer runs with `complyscan scan --changed-since <git-ref>`, model source context is narrower as well: every changed eligible code or technical-configuration file plus at most eight unchanged files connected within two locally indexed graph hops. Targeted batching exhausts structural candidates only inside that changed-plus-connected set; it does not widen back to the whole repository. The same boundary constrains broad review modes, technical investigations, and model-requested follow-up. Unrelated unchanged source is never sent, while repository-wide governance checks and framework reconciliation continue locally. Reports distinguish the complete local scan from this `changed-plus-connected` model scope.

Every finding has a stable SHA-256 fingerprint in structured output. Reviewed findings can be suppressed by `rule`, a Git-style `path` pattern, an exact `fingerprint`, or a combination. Every suppression requires a `reason`; suppressed findings are excluded from output and exit-code evaluation, and their count remains visible in the report summary.

For an existing repository, `complyscan baseline .` records the current deterministic findings and confirmed technical gaps in `.complyscan-baseline.json` without storing source evidence. Commit that deterministic file and future scans will report and gate only fingerprints that are new. A baseline accepts known work; it does not resolve it or establish compliance. Use `--baseline path/to/file` to select another baseline for a scan or `--no-baseline` to inspect every finding.

## Advisory AI layer

Interactive `complyscan setup` first inspects repository structure and technical signals locally without a model, then offers optional BYOK cloud review, model-free operation, and experimental local Ollama. It creates the report target from the repository name and does not send repository source or ask for profile classifications during setup. Saving a provider records `ai.review-on-scan: true` in the repository and matching trust outside it on the current machine, so the normal same-machine scan can infer code-visible facts during its advisory review after finishing the deterministic layer. Manual experimental Ollama selection remains available:

```bash
ollama serve
ollama pull qwen3.5:9b
complyscan scan . --provider ollama --ollama-model qwen3.5:9b
```

Editing `ai.provider` and `ai.review-on-scan` by hand records repository intent but does not create private trust on a new machine. Rerun guided setup to establish reusable local trust, or pass `--provider ollama` as explicit one-run consent. If the provider is unavailable, the deterministic report is preserved and the review is marked incomplete. Add `--require-ai-review` when an incomplete active review should return exit code `2`; the flag alone never grants consent. Use `--deterministic-only` to guarantee that a particular run does not contact Ollama or any hosted provider.

### BYOK remote providers

OpenAI, Anthropic, and Gemini use native adapters. xAI, Groq, Mistral, OpenRouter, and custom services share a schema-constrained OpenAI-compatible adapter. All use the same bounded prompts, binding checks, redaction, caching identity, and advisory-only result model as Ollama. Anthropic and Gemini receive provider-specific wire-schema normalizations limited to each documented structured-output subset; ComplyScan retains the complete original schema for trusted local validation, so transport portability does not relax result checks. The API key value is read from an environment variable at request time; it is never accepted as a configuration field, CLI value, report field, or cache field. Example:

```yaml
ai:
  provider: openai
  review-on-scan: true # repository intent; reusable automatic use also requires private local trust
  remote:
    model: gpt-5.6-sol
    api-key-env: OPENAI_API_KEY
    timeout-seconds: 360
    max-findings: 20
```

```bash
export OPENAI_API_KEY="your-key-from-your-secret-store"
complyscan scan . --provider openai --model gpt-5.6-sol --api-key-env OPENAI_API_KEY
```

That command's provider options are explicit one-run consent. The guided reusable equivalent is `complyscan setup`, which also stores a matching private trust record on this machine. The standard hosted submenu contains OpenAI, Anthropic, and Google Gemini only. After consent and selection of the API-key environment variable, setup queries account availability but displays only shortlisted model IDs; it never treats the full provider catalogue as quality-approved. Non-interactive remote configuration must include `--allow-remote-review`. Run `complyscan doctor --probe-review` for a small source-free compatibility check before reviewing sensitive repositories, and consult the [model support policy](docs/model-support-policy.md) for stronger live quality gates.

Custom compatible providers additionally store a human-readable `provider-name` and an absolute HTTPS `base-url`. URLs containing credentials, query parameters, or fragments are rejected. Preset endpoints are supplied by ComplyScan. The configured endpoint is included in the private model-qualification identity so results cannot be reused across gateways with the same model ID.

Remote repository analysis sends locally selected, secret-redacted structural candidate packages outside the machine when the configured AI layer runs, followed by synthesis when several source batches are needed. The same scan may send bounded secret-redacted finding records. Compatibility checks, schema repairs, and transient or rate-limit retries are provider requests too; retries of source-bearing work retransmit that bounded source context. Cached compatibility is not refreshed solely as a capacity probe; the first useful source request calibrates unknown capacity. All such calls may consume quota and incur variable cost, potentially faster when concurrency is available, and are governed by the provider account's processing and retention settings. ComplyScan sets `store: false` for native OpenAI and Gemini requests and refuses redirects. A custom compatible endpoint is an explicit additional trust boundary. These controls do not replace an organisational review of provider terms, endpoint ownership, region, retention, proxies, contracts, and permitted source-code processing.

Completed repository analysis and per-objective technical investigations are cached in the operating system's private user-cache directory, not in the scanned repository. Reuse requires the same active review-scope content, framework evidence, declared systems, ownership, confirmed-use review contexts, context strategy, token budget, provider endpoint, model, available qualified model-artifact digest, and prompt contract. Repository-analysis prompt version 17, private cache schema/file version 8, and repository-analysis cache context version 14 include atomic block-cited source observations, adaptive model-aware source sizing, compact response bounds, privacy-bounded per-attempt token/timing diagnostics, request-bound validation, one bounded schema repair, membership-only grouping, and untruncated local reattachment of validated evidence. Results created under an earlier contract cannot be reused. The repository cache file is `repository-analysis-v8.json`; per-objective technical observations use cache schema/file version 3 at `technical-review-v3.json` and bind the same available artifact digest. A full scan binds the full discovered repository; `scan --changed-since` binds its changed-plus-connected model scope while repository-wide framework evidence remains part of the identity. Cache files store validated model results plus cryptographic input digests, never the submitted source-context records, raw AI-use manifest, or credentials. Model rationales and citations may still describe repository details and should be treated as potentially sensitive. Only results with completed or unnecessary global grouping are cached; source-complete/grouping-incomplete results remain usable in that report but are automatically retried on a later scan. Interrupted multi-batch reviews currently restart the candidate queue because per-batch resume is not implemented. Cached coverage describes the original reviewed submissions; a cache-hit run transfers no repository source. Use `complyscan scan --refresh-review` to bypass both review caches and obtain fresh inference.

Ollama is an advanced experimental path. Its default candidate remains `qwen3.5:9b`; `qwen3:8b` retains only its exact recorded technical-review fixture history. Any other exact local tag may be selected and compatibility-qualified, but no local model currently earns the standard-review label. Setup may install Ollama, start its Homebrew service, or pull a selected model only after explicit confirmation; normal scans never install software or models. A future specialised downloadable model must pass the same maintained quality gates as the cloud shortlist before promotion.

ComplyScan uses separate repository and finding review flows. Repository `auto` and `targeted` modes locally queue every structural candidate-file excerpt. The queue uses one model call when it fits the per-request boundary; otherwise it uses as many bounded source batches as required and synthesizes their results. Independent source batches and independent groups within a synthesis level can run concurrently within the adaptive provider gate; a synthesis level starts only after its inputs have validated. `deep` retains automatic broad full-or-hierarchical analysis, `full` requires one broad request, `hierarchical` always partitions and synthesizes, and `bounded-only` disables repository-level reasoning while retaining the older per-objective technical investigation. Finding review is separately bounded by `max-findings` and sends visible, unsuppressed deterministic issue records with re-redacted metadata such as rule ID, fingerprint, title, message, relative path, line, short evidence, remediation, severity, and confidence. Informational AI-inventory detections are already deterministic and are not sent for a model to restate; an empty issue set makes no finding-review request. Submitted excerpts are re-redacted and are not copied into the saved report.

Ollama endpoints must be `localhost` or a loopback IP; ComplyScan bypasses HTTP proxies for that local route. Native remote adapters use fixed official HTTPS endpoints. Compatible presets use documented HTTPS API bases, while custom compatible providers require an explicit absolute HTTPS URL. Every remote adapter may respect an operator-configured HTTPS proxy and refuses redirects. Anthropic and Gemini receive non-mutating schema copies limited to their documented structured-output subsets; Gemini `const` constraints become one-value enums on the wire. Trusted local validation keeps and enforces the complete original schema. Repository strings, code, and comments are explicitly labelled untrusted in the fixed prompts. Finding output must preserve submitted identifiers; unknown, changed, duplicate, or malformed bindings fail that review. Repository-analysis output may select only submitted namespaced objectives and configured system IDs. Any citation outside the submitted file index or its valid line range fails the repository analysis.

Finding observations are attached to existing fingerprints as `confirmed`, `uncertain`, or `not_supported`. Technical investigations remain attached to the exact system, framework, objective, and evidence target. Candidate validation and missing-evidence search use only code assigned or shared with that system; conflicting and unassigned paths are excluded. Repository observations and per-use repository facts remain a separate advisory overlay and do not silently replace the configured system profile, human-owned AI-use register, or deterministic reconciliation. Compact source models return atomic observations without inventing candidate IDs; each citation uses a trusted request-local block ID and original line, and ComplyScan assigns scan-local observation identity after validation. Global synthesis decides only observation membership. All review types remain advisory and cannot change deterministic findings or CI gates. Terminal, Markdown, and schema-version 16 JSON expose repository-analysis coverage, grouping status, source batches started versus validated, total source batches, provider-request count, and follow-up metadata. JSON additionally records checked cross-batch gap resolutions and one privacy-bounded duration/outcome/retry/token diagnostic per repository-layer provider attempt for dashboard audit; these diagnostics contain no prompt, source content, file list, response body, or provider request ID. If every source batch validates but grouping fails, the report retains each observation separately and marks only AI-use organization incomplete; an incomplete source review retains accounting and coverage but no unvalidated conclusions. ComplyScan requests non-streaming schema-constrained output; Ollama additionally receives temperature zero, while modern remote models use their provider defaults where sampling parameters are unsupported or deprecated. Model output can still vary.

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
  # Use @main to test the unreleased unified workflow; pin the next release tag after publication.
  - uses: ComplyScan/ComplyScan@main
    with:
      severity: medium
```

The default `scope: auto` uses GitHub's base SHA on `pull_request` events. It change-scopes file-local findings and model context to changed-plus-connected files while repository-wide governance gates still run against the full local snapshot; ordinary non-PR events use full scope. Keep `fetch-depth: 0` so the base commit is available. If a normal pull request's base cannot be determined or resolved, the Action fails instead of silently widening model access to the full repository. Change-scoped `pull_request_target` runs are refused because that event's default checkout is the base branch and cannot safely represent the proposed changes; use a normal `pull_request` workflow for change scanning. A target-event workflow may consciously select `scope: full` only when a base-branch scan is actually intended. `scope: pull-request` requires a usable base, and `scope: full` is always a deliberate whole-repository choice.

The Action always invokes the unified `scan` command, but its first-class `ai-review` input defaults to `none` and forces deterministic-only operation even when the repository contains `ai.review-on-scan: true`. To permit source processing in CI, set `ai-review: configured` explicitly or, preferably, pin a provider and model with workflow inputs as shown below. `configured` deliberately trusts the provider destination and credential environment-variable name committed in `.complyscan.yml`. A missing key or unavailable model still leaves valid deterministic SARIF and reports; set `require-ai-review: true` only when model-layer completeness must be an operational requirement. With `ai-review: none`, that policy still runs the deterministic scan and saves reports before returning exit code `2`; it never grants processing consent. AI verdicts remain advisory and never change the deterministic finding threshold.

The concise Markdown report is appended to the GitHub job summary by default, including when a required AI layer is incomplete; set `publish-summary: false` to disable it. The Action exposes absolute `markdown-report` and `json-report` output paths for an explicit later artifact step, but never uploads the raw JSON evidence bundle automatically. SARIF upload is independent.

By default the Action fails after publishing available outputs when deterministic findings meet `fail-on`. Set `fail-on-findings: false` to publish alerts without failing the job, or `upload-results: false` when code-scanning upload is not available. `ai-review` accepts `none`, `configured`, `ollama`, `openai`, `anthropic`, `gemini`, `xai`, `groq`, `mistral`, `openrouter`, or `openai-compatible`. Model, endpoint, and routing inputs are supported one-run overrides. Native OpenAI, Anthropic, and Gemini use their fixed credential environment names unless `api-key-env` overrides one. xAI, Groq, Mistral, and OpenRouter require an explicit workflow credential name and use ComplyScan's known provider base unless `base-url` overrides it; custom `openai-compatible` additionally requires both `base-url` and `provider-name`. The old `review` input remains only as a deprecated alias and conflicts with a non-default `ai-review`. Credential values belong in GitHub Actions secrets exposed through `env`—never in an action input or committed YAML.

```yaml
- uses: ComplyScan/ComplyScan@main
  env:
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
  with:
    ai-review: openai
    model: gpt-5.6-sol
    api-key-env: OPENAI_API_KEY
```

Pin the provider and model in a protected workflow and review configuration changes like code. `ai-review: configured` is shorter, but deliberately trusts the provider identity stored in the checked-out repository. The current stable v0.1.7 Action retains the older explicit-review behavior; the unified scope and scan behavior above remain unreleased until the next tag is published.

## Privacy and security guarantees

Every `complyscan scan` runs the deterministic layer locally, collects no telemetry, and never prints a complete detected secret. `complyscan scan --deterministic-only` additionally guarantees that no model is contacted, no provider credential is read, and no source leaves the machine.

`complyscan setup` is a separate provisioning boundary. After explicit confirmations it may invoke Homebrew, download and run Ollama's official Linux installer, start the Homebrew Ollama service, and use `ollama pull` to download the selected model. Repository context reaches a model only after the user has selected the local or hosted privacy boundary and the model has passed compatibility checking; setup may then use bounded, redacted context for editable answer suggestions. Non-interactive setup performs no installation or model download unless the corresponding install or pull flag is supplied.

Permission errors are reported as warnings where scanning can safely continue. Source excerpts are short and pass through credential redaction before appearing as evidence.

When guided setup records both repository intent and matching private machine trust, later same-machine scans may send one or more best-effort-redacted structural candidate packages, synthesis context, and bounded finding records to that configured provider and may incur variable cost. Metered compatibility checks, validation repairs, and transient or rate-limit retries can add requests; source-bearing retries resend the bounded package. Cached compatibility is not repeated only to obtain capacity headers. Concurrency may consume the same variable quota and cost more quickly, while the finite retry and request ceilings mean permanent provider, quota, or schema failures can still leave the AI layer incomplete. A clone receives the repository intent but not that trust and therefore remains deterministic until the user runs setup or supplies explicit one-run provider options. Ollama requests use a validated loopback endpoint; hosted requests use the selected HTTPS endpoint after setup disclosure and consent. The active ComplyScan configuration is excluded from model context, although separately constructed typed system facts may be supplied. Submitted source excerpts are not stored in reports or the OS user-cache, but model rationales may describe repository details. Missing AI dependencies preserve deterministic results. CLI reports remain local unless an explicit integration publishes them; the GitHub Action behavior is described below, and no dashboard connection exists yet.

The optional GitHub Action uploads SARIF metadata to GitHub code scanning when `upload-results` is enabled. That SARIF contains finding messages, repository-relative paths, line numbers, and fingerprints, but not source excerpts or detected credentials. It also appends the concise Markdown report to the job summary unless `publish-summary: false`; the raw JSON bundle is never uploaded automatically. When the configured AI layer runs, SARIF and the Markdown report may include advisory verdict, confidence, rationale, suggested action, provider, and model metadata. Treat those summaries as potentially sensitive repository metadata.

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

Experimental compliance-owner profile assistance has a separate labelled quality gate under `testdata/profile-draft-evaluation`; ordinary setup does not invoke it. Run `./scripts/validate-profile-draft.sh` to test the current Ollama candidate against expected profile facts, a documentation-only hard negative, forbidden inferences, grounded citations, and explicit precision/recall/time thresholds. The full four-case run may take up to 20 minutes; `COMPLYSCAN_PROFILE_DRAFT_CASES="documentation-hard-negative"` selects a quick single case. See [profile-draft validation](docs/profile-draft-validation.md).

ComplyScan applies the same evidence discipline to itself. Its maintained [AI applicability assessment](docs/AI_SYSTEM.md) distinguishes deterministic-only mode from local and remote inference-enabled configurations and records the required pre-release reassessment. The companion [technical risk assessment](docs/risk-assessment.md) records foreseeable harms, controls, residual risks, and review expectations. These documents support governance; they are not self-certification.

## Roadmap

Future releases may add:

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

Ollama, OpenAI, Anthropic, Gemini, xAI, Groq, Mistral, OpenRouter, and custom OpenAI-compatible APIs remain implemented review providers for backward compatibility and explicit automation. Standard interactive setup recommends OpenAI, Anthropic, or Gemini from a bounded model shortlist; other hosted integrations and Ollama are experimental. The built-in scan default remains `none`. Every hosted provider requires an explicit external-processing confirmation, an environment-only API key, and a user-selected model before any profile-draft context is submitted.

## Disclaimer

ComplyScan findings are automated technical observations. They are not legal advice, a legal determination, a conformity assessment, or compliance certification. Regulatory obligations depend on the complete AI system, its intended purpose, deployment context, actors, data, and real-world use. Obtain qualified legal and compliance review for decisions about the EU AI Act or other laws.

Licensed under the [Apache License 2.0](LICENSE).
