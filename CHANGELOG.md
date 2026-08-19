# Changelog

## 0.2.0 - 2026-08-18

### Added

- A strict, human-owned version-1 AI-use register at `.complyscan/ai-uses.yml`, plus `complyscan ai-uses show`, the explicit `complyscan ai-uses setup` workflow for confirming, merging, dismissing, or deferring repository-analysis suggestions, and `complyscan ai-uses edit` for revising an existing saved use without scanning or contacting a model. Edits preserve stable identity and linked suggestions; model-authored suggestion IDs never become durable project identity, and path overlap alone cannot silently merge distinct AI uses.
- An automatic positive repository-fact overlay for each exact saved or model-suggested AI use. Deterministic analysis records scoped runtime provider references without promoting an SDK import or endpoint string to an AI activity. The optional model pass can add typed activity and other facts only with checked citations inside that use's submitted scope; a scoped command or installer becomes a `local-cli` fact only after positive AI activity is supported. Conditional `possible` provider, downstream-provider, and deployer candidates explain what distribution and integration artifacts could indicate without asserting a statutory role.

### Changed

- The default Markdown is now a one-page developer action plan: result, selected-framework coverage, at most three prioritized actions with explicit do/why/where fields, a compact code-control table, the AI functionality found, repository-invisible questions, and scan coverage. The framework summary names mapped provisions and counts code signals found, no matching signals, and checks that could not be completed; control results and actions retain their framework/provision label without presenting a legal score. Role hypotheses, complete fact tables, legal-source detail, and raw model/accounting records remain in schema-version 16 JSON for dashboards and audits instead of expanding `latest.md`. The model supplies structured cited observations, while local deterministic code controls evidence validation, report counts, priorities, labels, and layout.
- Global repository synthesis now asks the model only for observation grouping and concise use labels. Bounded source calls still produce and validate the full evidence records, but synthesis receives a compact view and returns no repeated facts, citations, objectives, unmapped observations, or questions. ComplyScan derives the final membership-based ID and reattaches those checked records locally. This removes the dominant synthesis output and validation overhead while keeping the model authoritative over grouping.
- Targeted hosted source requests now choose an adaptive input target from the configured limit, a confidently known model context window, the selected evidence size, and live provider token capacity. Small selections remain one modest request; medium long-context selections use a balanced 48,000-token target and can grow to 64,000 tokens when needed, normally producing about two source bundles before one compact grouping synthesis. The packer balances the complete candidate queue before transfer instead of greedily filling one bundle and later splitting it. Unknown models and compatible gateways retain a conservative ceiling. Source responses use tighter text, citation, question, and unmapped-observation bounds so they return facts and checked evidence instead of mini-reports. Resolved calls, repository imports, and confirmed-use scope preferentially keep connected files in one context bundle when they fit; these bundles are not deterministic AI-use classifications, and persistent validation failures fall back to smaller per-file packages.
- Ordinary guided setup now asks only for the optional AI provider/model, selected technical mappings, and the final save/run action. It creates one draft report target from the repository name without asking developers to classify organisation roles, decision impact, oversight, activities, deployment, data, or legal applicability. Positive code-visible facts are inferred during scanning and remain cited report observations; repository-invisible organisation context remains an explicit non-blocking report unknown. The full questionnaire remains available only through `setup --advanced` and `profile setup` for compliance-owner workflows.
- Default `auto` and `targeted` repository analysis now queue one bounded excerpt from every structural candidate file instead of treating a per-request target as a repository-wide evidence cap. Small queues still use one request; larger queues use bounded source batches and synthesis, and changed-since scans exhaust candidates only inside their changed-plus-connected model boundary. A completed review may report zero model-identified AI uses only after every candidate batch validates. If all source batches validate but global grouping fails, every checked observation remains separately actionable and only grouping is marked incomplete; incomplete source reviews retain accounting but no unvalidated conclusions. Per-batch persistence and resume are not implemented yet. The private repository-analysis cache uses schema/file version 8 and context version 14, stored as `repository-analysis-v8.json`, and caches only results with completed or unnecessary grouping. The technical-review cache uses schema/file version 3 at `technical-review-v3.json`; both bind an available qualified model-artifact digest so an updated Ollama tag cannot reuse an older model's conclusions. Machine-local consent remains bound to context contract `adaptive-provider-pipeline-v5` because the selected source scope did not expand.
- Independent source batches, output-exhausted members of the same completed source wave, and—after source validation—independent groups within each synthesis level use one provider-neutral adaptive scheduler. When both request-per-minute and token-per-minute response headers are available, both dimensions bound each concurrent wave; partial capacity remains sequential. Header-free hosted runs start at one useful source request and ramp conservatively after successful waves, while a local ceiling of 32 prevents unbounded socket concurrency; Ollama remains serial because it exposes no portable capacity metadata. Cached compatibility is no longer refreshed with two synthetic contracts merely to obtain headers, removing normally two calls from a multi-batch run. Provider-wide validation repairs, transient failures, and rate limits use jittered bounded retries through the same gate; permanent quota failures return promptly, cumulative automatic wait is capped at ten minutes per logical request, transient retries at eight cycles, and repository analysis at 256 total provider requests. These bounds preserve deterministic output but cannot guarantee a completed AI result under permanent provider or schema failure.
- Live source-free model qualification now exercises both finding-binding and repository-shaped structured contracts. The two phases normally use two requests and share a four-request ceiling for typed temporary retries; permanent quota, request-size, cancellation, and contract failures stop promptly. Finding review, technical-search planning, and technical evidence judgments use the same bounded provider-neutral taxonomy; metered failed attempts and validated partial observations are preserved, and required AI review now fails honestly when requested findings or technical targets remain unreviewed. Deterministic informational AI-inventory notices do not trigger a separate model call merely to restate them, and an empty issue set makes zero finding-review requests. Scan, setup, and doctor output retain exact request and token accounting. Native Gemini keeps trusted system instructions separate from untrusted repository input; Anthropic context overflow is adaptively split, and Anthropic/OpenAI-compatible reasoning-token details are retained where providers report them.
- Scan evidence bundles now use schema version 16. Version 16 adds `grouping_status` plus exact per-attempt input/output/reasoning token counts; version 15 added duration/outcome/retry request diagnostics, version 14 added checked cross-batch gap-resolution records, and version 13 added `provider_requests` and `source_batches_started`. Source-complete/grouping-incomplete runs retain validated observations, while incomplete source runs retain accounting and coverage but no unvalidated semantic conclusions. These observations and unknowns do not create setup tasks, mutate profiles or the AI-use register, decide applicability, or affect CI gates.
- Repository-analysis prompt version 17 replaces source-batch candidate/fact cross-references with atomic observations. Source responses cite trusted request-local block IDs and original lines; ComplyScan assigns observation identity and unions checked nested fact citations locally. One bounded corrective regeneration remains for genuinely malformed output before smaller splitting. Membership-only synthesis must assign every trusted observation exactly once and may resolve a batch-local question only with another member observation in the same group plus one of that observation's checked citations. Local validation rejects invented claims, self-resolution, unrelated groups, and citations outside the resolver. ComplyScan derives `inferred-use-*` IDs from sorted membership and reattaches the complete checked records. The private repository-analysis cache is schema/file 8 with context 14.
- Automatic model qualification identity now includes qualification contract version 3, repository-analysis prompt version 17, and setup-draft prompt version 6. Its private cache is schema/file 5 at `model-qualification-v5.json`, so an older candidate-ID repository contract cannot authorize source transfer.
- Setup-draft prompt version 6 shares the same narrower repository-evident field vocabulary and uses the portable disjoint `anyOf` fact union; deployment suggestions are limited to `embedded`, `api`, and `local-cli`, so repository prose cannot establish internal, customer-private, public, or open-source operation.
- Unified scans load the optional AI-use register locally without mutating it, exclude the exact manifest from discovery and model context, and report confirmed, draft, retired, suggested, and ungrouped observations separately. Confirming or editing those groupings is an optional precision step, not a prerequisite for a useful report. Changed-since observations cannot delete or retire saved uses, and confirmation remains a repository grouping rather than a compliance conclusion.
- Schema version 9 introduced the top-level `ai_use_inventory` overlay, per-confirmed-use `ai_use_mappings`, explicit `repository_analysis_run` lifecycle, and `repository_analysis.result.objective_observations[].ai_use_id`; versions 10 through 13 retain those contracts without changing deterministic findings or CI gating.
- Active developer-confirmed AI uses are mapped independently to framework objectives using their saved path scope and each associated configured system's declared context. Signals found only outside a use are retained separately to expose shared code or incomplete scopes. Missing system associations leave legal and other context-dependent checks unresolved while framework-wide voluntary practices may remain recommendations; multi-system uses are evaluated once per context.
- Configured repository review now supplies bounded typed contexts for active confirmed uses: stable human-owned IDs, descriptive path scopes, valid system associations, exact submitted files, and likely-required or recommended objective contexts. The model evaluates each use/objective/system combination separately in every applicable source request, and ComplyScan preserves and conservatively merges those validated records locally after grouping. Definite decisions require checked citations inside that use's submitted files; an uncited result can only remain uncertain. Missing combinations, generic ambiguous claims, and unsafe citations reject or remain outside the use mapping, while valid direct observations can distinguish intentionally overlapping uses. The raw register remains local and model output cannot mutate it.
- Configured AI review now reuses exact-match repository-analysis results from a private cache that omits submitted source-context records, the raw AI-use manifest, and credentials. Active review-scope content, framework evidence, profiles, ownership, derived confirmed-use IDs/path scopes/system-objective bindings, provider endpoint, model, prompt, strategy, or budget changes invalidate reuse; the deterministic selector derives the submitted-file set from those bound inputs. Full scans bind the full discovered repository, while `scan --changed-since` binds its changed-plus-connected model scope. A confirmed use with no selected file in that scoped run receives no direct model context. `--refresh-review` forces fresh inference, reports identify cache hits, and cached model summaries or citations remain potentially sensitive.
- `complyscan scan` is now the single normal workflow: it always runs deterministic checks and runs the saved advisory provider only when `ai.review-on-scan: true` is backed by matching private trust recorded by guided setup on that machine. The trust identity binds provider, endpoint, model, and credential environment-variable name, so a clone or configuration change cannot silently authorize source transfer. Provider/model/endpoint/refresh/deep/review options remain explicit one-run consent; `--deterministic-only` guarantees no model or credential use. Provider failures preserve the deterministic report. `--require-ai-review` can enforce completeness but never grants consent by itself. Legacy or untrusted configurations keep deterministic scans and receive migration guidance; `complyscan review` remains a hidden compatibility command.
- `complyscan doctor` now treats missing optional reviewer credentials, services, and models as warnings while deterministic scanning remains available. `doctor --probe-review` retains strict readiness semantics: a missing required dependency is a blocking failure and the live compatibility request is skipped.
- Guided setup's first run and the GitHub Action now use the unified scan. Setup saves repository intent and private local trust only after its privacy disclosure. The Action's new first-class `ai-review` input defaults to `none` and forces deterministic-only operation; `configured` or a provider value is explicit one-run CI consent, while `require-ai-review` remains policy only. `scope: auto` uses changed-plus-connected scope for normal pull requests when their base is available and full scope for ordinary non-PR events; a missing normal pull-request base fails instead of silently widening access. Change-scoped `pull_request_target` runs are refused; a workflow may consciously request only a full base-branch scan there. Model verdicts stay advisory, and only deterministic findings affect the finding threshold.
- The composite Action now appends the concise Markdown report to the GitHub job summary by default, exposes the local latest Markdown and JSON paths as outputs, and never uploads raw JSON automatically. Report publication remains available before operational and threshold enforcement and is independent of SARIF upload; `publish-summary: false` disables the summary.
- The composite GitHub Action now runs on macOS Bash 3.2 and uploads any valid deterministic SARIF before enforcing an operational or incomplete-required-review failure, preserving code-scanning evidence while retaining exit code `2`.
- Guided setup now recommends BYOK cloud review, retains model-free technical analysis, and moves Ollama behind an explicitly experimental advanced choice. The standard provider picker contains only OpenAI, Anthropic, and Google Gemini.
- Standard cloud setup now exposes two exact quality-oriented candidates per provider, filters live account catalogues against that shortlist, and shows separate setup-draft and technical-review benchmark status. Compatibility no longer appears as a substitute for measured quality; explicit automation and existing configurations retain experimental provider and model compatibility.
- Guided setup questions now support the physical ← key wherever a previous answer can be revisited, without adding a Back row to selector choices. The accessible and redirected text interface accepts `back`. Completed answers remain selected or become the text default when revisited, while `Ctrl+C` continues to cancel setup.
- Guided setup now places controlled-choice definitions directly beside every applicable answer and shows examples, warnings, and free-text guidance automatically. The separate `Further explanation` row and `? details` interaction have been removed; the former `--detailed-guidance` flag remains accepted as a deprecated no-op for command compatibility.
- Ordinary guided setup now presents four clearly named overall steps without a system questionnaire. The explicit advanced questionnaire labels each prompt as `Question X of Y`; conditional EU follow-up totals are calculated only after the relevant technical answers are known.
- Advanced questionnaire choice prompts do not preselect any answer. Repository-derived and AI-drafted suggestions remain advisory guidance in that explicit workflow, and every categorical answer requires a compliance owner to select `unknown` when the fact is not established.
- Local model setup now keeps installed and recommended models concise, offers an offline curated catalogue of common exact Ollama tags grouped by purpose with download sizes in GB, and reserves `Exact tag` for every other Ollama model. This avoids noisy community search results and setup-time catalogue requests; cloud-only variants remain excluded from local mode, and every unseen model still has to pass the source-free compatibility check before repository context is sent.
- The hosted adapter layer supports OpenAI, Anthropic, Google Gemini, xAI, Mistral, GroqCloud, OpenRouter, and custom HTTPS OpenAI-compatible APIs. Standard setup narrows that architecture to the three native providers and their quality-oriented shortlist; the remaining integrations and exact-ID entry are retained for explicit experimental configuration. Credentials remain environment-only, and the source-free compatibility check still runs before repository context is sent.

### Fixed

- OpenAI repository review and setup drafting now express field-specific structured-output variants with the supported nested `anyOf` keyword instead of unsupported `oneOf`. Trusted local validation still enforces the exact fact field/value pairing after every provider response.
- Native Anthropic and Gemini requests now receive provider-specific copies of the JSON schema normalized to each documented wire subset. Unsupported transport-only validation keywords are omitted or translated without modifying the original schema or weakening trusted local Go validation.
- Repository analysis now feeds precise trusted-validation failures back to the same provider for a finite repair attempt, retries transient and rate-limited calls without OpenAI-specific assumptions, recognizes permanent quota exhaustion as non-retryable, and accounts every probe, source request, synthesis request, repair, and retry against its safety ceiling.

## 0.1.7 - 2026-08-11

### Changed

- Interactive setup, profile, ownership, and verification wizards now render their main section titles as consistent bold terminal headings while preserving plain output for accessible, dumb, redirected, and `NO_COLOR` modes.
- Guided setup now shows five numbered stages from privacy selection through review and first scan, so users can see where they are and how much remains.
- Local model selection now shows transparent download/runtime-memory ranges, model pulls retain Ollama's live progress and report elapsed time, and qualification, profile drafting, finding review, and framework evidence investigation report elapsed and target progress with the actual provider name.
- Guided setup now presents a final summary of analysis privacy, model readiness, frameworks, systems, evidence ownership, and first-run behavior before writing configuration; users can revisit any major section or cancel without saving.
- Interactive terminals now default to concise, progressive question guidance and offer a detailed mode with all category definitions and examples; `--detailed-guidance` makes the complete explanations directly selectable for later runs and accessible text-mode sessions retain them by default.
- Setup review and completion now use a consistent status vocabulary—ready, needs review, and not configured—with compact visual markers in styled terminals and explicit text markers in accessible or redirected output.
- Guided setup now saves private, source-free recovery checkpoints in the operating system's user-cache directory after each major stage, offers to resume or discard them on the next terminal run, and removes the draft after configuration is saved.

### Fixed

- Setup cancellation now advertises recovery only when a private checkpoint was actually written successfully.

## 0.1.6 - 2026-08-11

### Added

- Automatic one-request model qualification for arbitrary Ollama tags and supported BYOK model IDs, with no repository data, strict structured-record binding, an instruction-shaped hard negative, a private 30-day cache keyed by model/digest and prompt contracts, setup/deep-scan enforcement, doctor status, and deterministic fallback.

### Changed

- Guided setup now uses keyboard-controlled radio menus for fixed single choices, checkboxes for multiple choices, highlighted Yes/No confirmations, and selectable local or remote model lists with an explicit custom-model entry. This also covers system, test-command, and objective selection in verification setup; redirected input, dumb terminals, and `COMPLYSCAN_ACCESSIBLE=1` retain the stable numbered or text fallback.
- First-run setup now establishes its local, cloud, or model-free privacy boundary before repository analysis, uses a ready reviewer to draft only repository-evident profile answers with confidence and cited paths, and presents every draft as an editable human-confirmed default. Jurisdiction, organisation role, actual production use, legal applicability, negative data claims, and other facts that source code cannot establish remain direct human questions.
- The final first-run scan reuses the bounded repository discovery already completed during setup rather than traversing and classifying the same files a second time; code-graph construction and framework evaluation still run once against the confirmed profile.

### Testing

- A labelled AI-assisted-onboarding benchmark measures controlled-field precision and recall, forbidden inferences, grounded citations, and per-case time across three positive repository shapes and a documentation-only hard negative. A live Ollama harness saves machine-readable results and resource measurements without making model execution part of CI.
- Profile-draft prompt version 4 passed the complete `qwen3.5:9b` onboarding gate with 88.2% precision, 88.2% recall, zero forbidden claims, zero ungrounded references, and all four cases within their time limit; the separate technical-review gate remains pending.

## 0.1.5 - 2026-08-10

### Changed

- The recommended Ollama default is now `qwen3.5:9b`; `qwen3:8b` remains selectable as the previously validated model, and the new default is explicitly marked as pending the maintained live quality gate.
- Interactive setup now uses numbered menus for framework, profile, review-provider, ownership, and verification choices; multi-select questions accept comma-separated numbers instead of requiring internal values.
- First-run setup now inspects and summarizes the repository before asking questions, recommends a technical framework mapping from declared regions, and follows the short screening with only the additional EU applicability questions made relevant by earlier answers. The complete unbranched questionnaire and attributed legal decision record remain available through `complyscan setup --advanced`.
- EU reports now label technical mapping context as `incomplete`, `factually-ready`, or `human-reviewed`, list unresolved facts, and keep requirement mappings explicitly provisional when inputs are missing.
- First-run setup now offers an explicit quick scan, deep AI review, or configure-only choice. `complyscan scan --quick` guarantees a model-free preliminary scan, while `--deep` requires a configured review provider.
- Running `complyscan` without a subcommand now starts discovery-first setup when no repository configuration exists and scans immediately when the current repository is already configured.
- Deep scans now save a complete preliminary Markdown and JSON report before the first model request, checkpoint completed framework reviews, and preserve deterministic results when advisory finding review is unavailable.
- A failed or invalid model response now marks only that technical target as incomplete and continues with the remaining evidence investigations instead of aborting the entire scan.
- Deep review now reuses deterministic graph and source retrieval directly, asks the model to plan follow-up searches only for context-poor investigations, and bounds local-model output budgets to reduce unnecessary generation time.
- Advisory review now inspects at most two representative code candidates per system and technical objective, while retaining every deterministic match in the report and disclosing any omitted repetitive model targets.
- Terminal scans now finish with a concise AI inventory, technical-objective, requirement-mapping, and advisory-review summary; `--verbose` restores the full framework and evidence dump while reports always retain complete detail.
- Findings now identify production, test, documentation/example, or configuration scope. Ambiguous generic `message` logging signals are medium severity, while explicit prompt, response, or model-output logging remains high severity, and additional obvious token placeholders are ignored.
- Routine `complyscan` and `complyscan scan` runs are now deterministic by default even when a reviewer is configured; model execution requires `--deep` or an explicit `--review` override.

## 0.1.4 - 2026-08-08

### Added

- A built-in, code-only NIST AI RMF 1.0 technical evidence pack with 11 inspectable objectives and explicit voluntary-framework semantics.
- Framework selection in configuration and guided setup, including repeatable `--framework` automation flags and EU-specific applicability questions only when the EU pack is selected.
- Canonical technical control IDs that let different framework objectives reuse repository evidence and stable fingerprints without conflating their citations or legal nature.
- Per-framework terminal, Markdown, and schema-version 5 JSON results, plus selected-framework objective choices in guided verification setup.
- Validated, version-controlled path ownership rules that assign repository evidence to one declared AI system or intentionally share it across several systems.
- Guided `complyscan ownership setup` and inspectable `complyscan ownership show` commands, also offered during multi-system setup.
- Per-reference ownership status in terminal, Markdown, and schema-version 5 JSON reports, including assigned, shared, conflicting, unassigned, and disclosed single-system inference states.

### Changed

- Scans now discover and inventory a repository once, then evaluate every configured technical pack and label NIST results as voluntary recommendations rather than likely legal requirements.
- The EU AI Act technical pack is version 0.1.3 with schema-version 2 applicability fields and canonical control mappings; its measured evidence-match terms remain unchanged.
- The technical-review prompt contract is version 10 and binds review targets to their selected framework pack.
- Reconciliation now maps objective evidence and observed AI components only to systems that own the matching repository path; conflicting and unmatched paths remain explicitly unresolved.
- Advisory candidate and missing-evidence investigations are now scoped per system. Graph construction, initial retrieval, model-directed follow-up, cache entries, observations, isolated-test context, and reconciliation use only assigned or intentionally shared paths.
- The source-free cache uses schema version 2 so earlier unscoped observations cannot be reused.

### Testing

- Multi-framework regressions cover configuration validation, setup branching, report rendering, recommendation semantics, shared-control fingerprints, and verification objective selection.
- A source-free NIST synthetic benchmark covers all 11 objectives, four indexed-language case types, production and test-only reachability, required graph relationships, and split-keyword hard negatives with enforced CI thresholds.
- Independently reviewed NIST labels over ten exact public-repository revisions record 32 reasonable candidates, five false positives, 86.5% candidate precision, and complete recall against the labelled paths without storing third-party source.
- A cache-bypassed prompt-version 10 `qwen3:8b` gate passed across Go, Python, and TypeScript with independently validated EU and NIST targets, bounded prompt-injection fixtures, and no semantic guardrail corrections; a controlled awake TypeScript rerun completed in 592.9 seconds.

## 0.1.3 - 2026-08-06

### Added

- `complyscan doctor` for offline build, repository, configuration, report-permission, Git, and optional Ollama readiness checks.
- A repeatable `qwen3:8b` live-validation harness with enforced production/test-only expectations and saved resource metrics.
- Go, Python, JavaScript, and TypeScript repository-graph context for technical evidence, including routes, authorization, persistence, logging, configuration, and production/test reachability.
- A versioned technical-evidence benchmark with labelled multi-language repository cases, hard negatives, machine-readable metrics, and CI acceptance thresholds.
- A source-free external benchmark that verifies and scans exact commits of three permissively licensed public AI repositories, with recorded provenance and human candidate labels.
- An opt-in `qwen3:8b` semantic benchmark over the pinned public-repository candidates, with source-free decision output, explicit acceptance policy, quality thresholds, and focused candidate debugging.
- A private, source-context-free technical-review cache in the operating system's user-cache directory, with content-aware invalidation, atomic writes, and `--refresh-review` bypass support.
- Live per-candidate technical-review progress that distinguishes Ollama inference from cache reuse.
- Explicit system activity declarations for inference, training, fine-tuning, evaluation, automated decisions, agent tool use, and synthetic-content generation.
- Versioned deterministic reconciliation between screened EU AI Act technical objectives and independently discovered repository evidence, including visible non-detections, mismatches, and unassigned evidence.
- Developer-focused explanations, category definitions, and examples before every interactive setup question, including a safe `needs-review` default for human applicability.
- EU AI Act technical pack `0.1.2` with strictly validated, inspectable applicability scope, activity, and external-use conditions beside every code-evidence objective.
- Bounded model investigation of technical candidates and likely-required objectives where deterministic evidence was not detected, including one validated follow-up retrieval round over eligible repository files.
- Source-grounded model conclusions, supporting and contradicting evidence, unresolved questions, deterministic semantic guardrails, and separately reported evidence assurance.
- Opt-in isolated Docker or Podman verification that runs explicitly configured tests without a shell, network, writable repository mount, or elevated container privileges.
- Reusable verification recipes and guided setup that connect bounded test results to selected technical objectives without turning those results into legal conclusions.
- An Ollama model picker that lists installed and recommended models while accepting any exact local model tag.
- BYOK OpenAI, Anthropic, and Gemini model review through fixed official endpoints, schema-constrained output, explicit remote-processing consent, and environment-only credentials.
- Remote-provider readiness checks and an explicit `doctor --probe-review` synthetic compatibility request.
- GitHub Action inputs for local or BYOK model review without accepting API-key values as action inputs.

### Changed

- The public Go module and source references now use `github.com/ComplyScan/ComplyScan`.
- CI now verifies the documented Go 1.22 minimum.
- Technical evidence now uses bounded keyword boundaries and local line context, preserves camel-case path signals, and applies the refined EU AI Act technical pack `0.1.2` terms derived from public-repository false-positive review.
- Reconciliation now consumes applicability conditions directly from the versioned technical pack instead of maintaining a second hard-coded Go mapping.
- Technical review now evaluates one candidate per model request and binds the returned decision to the objective and evidence fingerprint in trusted code instead of asking the model to reproduce identifiers.
- Technical review context includes a wider bounded window around the actual match before connected graph context, improving evidence available for mechanisms whose implementation spans nearby helpers.
- Transparent semantic guardrails retain executable grader and rubric implementations while rejecting discussion-only website, documentation, FAQ, and quiz components.
- Scan evidence bundles now use schema version 3 and include the AI component inventory, requirement-to-evidence reconciliation, isolated verification assurance, and advisory model observations in JSON, Markdown, and terminal output.
- Interactive setup now offers deterministic-only, local Ollama, or explicitly consented BYOK review and stores only the selected remote credential's environment-variable name.
- All review providers share the same bounded context, identifier binding, redaction, cache identity, and advisory-only result contract.

### Fixed

- Documentation values explicitly labelled as example, replacement, dummy, fake, or sample credentials no longer fail self-scans, while generic high-entropy assignments remain reportable.
- Malformed optional model search plans are safely skipped instead of aborting an otherwise valid bounded investigation.
- Negative technical conclusions require grounded context and cannot infer missing authorization or implementation evidence from an isolated excerpt.

### Testing

- Installer regressions cover unsupported systems, incomplete releases, and preservation of existing binaries after failed updates.
- Offline Ollama tests cover prompt injection, malformed output, invalid verdicts, duplicate bindings, and timeouts without downloading a model.
- Technical-evidence regression metrics cover candidate precision and recall, anchor and reachability accuracy, relationship recall, forbidden relationships, and language coverage.
- The pinned public-repository study records deterministic retrieval and live semantic precision, recall, specificity, coverage, token usage, and model disagreements separately.
- Regression tests cover the pinned study's medical-bias rubric false negative and interactive safety-quiz false positive; live validation explicitly bypasses cached observations.
- Three fresh `qwen3:8b` public-repository runs record identical effective decisions and worst-case raw-model versus post-guardrail precision, recall, specificity, coverage, duration, and variability without committing source or full rationales.

## 0.1.2 - 2026-08-03

### Added

- Aggregated AI provider and framework inventory with representative locations.
- Bounded discovery, nested-repository boundaries, Git-tracked-only scans, exclusions, and terminal progress.
- Stable finding fingerprints, reasoned suppressions, and source-free baselines.
- SARIF 2.1.0 output and a reusable GitHub code-scanning action.
- Cross-platform release archives, SHA-256 checksums, and build attestations.
- Maintained AI applicability and technical risk assessments for ComplyScan itself.
- Typed AI-component signals for recognised dependencies, imports, endpoints, and environment-variable access.
- A labelled positive and hard-negative evaluation corpus with enforced precision and recall thresholds.
- A structured `inventory` command with terminal and versioned JSON reports.
- `generate ai-system` and `generate risk-assessment` commands that create reviewable, inventory-prefilled governance scaffolds.
- `scan --changed-since <git-ref>` and a matching GitHub Action input for pull-request scope, including local staged, unstaged, and untracked files.
- Explicitly enabled, loopback-only Ollama review of sanitised deterministic findings using schema-constrained output.
- Advisory observations in terminal, JSON, and SARIF without changing deterministic findings or exit status.
- Guided multi-system profiles covering intended purpose, regions, roles, use-case domains, people, data, deployment, oversight, and attributable human applicability decisions.
- Conservative EU AI Act scope and possible high-risk screening in `profile show`, terminal scans, and JSON reports.
- `profile setup` for adding or replacing system context in an existing configuration.
- A content-addressed `eu-ai-act-technical-evidence` v0.1.0 pack with 13 code-only objectives associated with Articles 9, 10, 12, 14, 15, and 50.
- `framework list` and profile-independent `framework assess` commands with terminal and versioned JSON evidence reports.
- Conservative `candidate-evidence`, `not-detected`, and `not-evaluated` objective statuses without control-level compliance rollups.
- Versioned scan identity, timestamp, build identity, and explicit finding/evidence scope in JSON bundles.
- Automatic atomic `.complyscan/reports/latest.md` and `latest.json` output, with `--no-report` and safe target-relative `--report-dir` overrides.
- Git-ignore initialization and built-in discovery exclusion for generated scan reports.
- A unified `setup` wizard for system context, human applicability decisions, local model selection, consent-based Ollama provisioning, and an optional first scan.
- A one-command macOS/Linux installer with platform detection, mandatory release checksum verification, user-local installation, pinned versions, and automatic guided-setup handoff.
- A minimal GitHub Pages deployment that publishes the installer at `complyscan.github.io/ComplyScan/install.sh` without exposing the repository as site content.

### Changed

- Terminal findings are emitted live while rules run.
- Scan-wide AI analysis is shared across rules to avoid repeated repository work.
- AI discovery now requires technical evidence instead of matching plain provider names.
- Changed-since scans keep documentation and risk-evidence checks repository-wide while scoping code rules to changed files.
- Forced configuration updates are validated and written atomically while preserving file permissions.
- Changed-since scans retain the complete repository snapshot for technical and governance evidence while recording the narrower finding scope.

### Fixed

- Public repository references and installation flow.
- Secret detection no longer treats ordinary hyphenated words ending in `sk-...` as credentials.
- ComplyScan's detector signatures and synthetic fixtures no longer appear as AI components in its self-inventory.
