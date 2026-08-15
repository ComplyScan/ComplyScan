# Repository AI code analysis

`complyscan review` uses a hybrid analysis pipeline. Local deterministic code first discovers the repository, inventories AI signals, maps technical-objective matches, identifies production entry points, and builds a code graph. The model then receives a compact evidence package built from those anchors and their connected code. Plain `complyscan scan` stops after the deterministic layer and never enters this pipeline.

## Default pipeline

1. ComplyScan loads its active configuration locally and excludes that exact file from discovery and model context.
2. Discovery applies the existing `.gitignore`, exclusion, size, count, binary, symlink, dependency-directory, generated-output, and nested-repository boundaries.
3. Local code classifies files, redacts recognised credential formats, inventories likely AI integrations, applies technical evidence rules, and builds the Go, Python, JavaScript, and TypeScript repository graph.
4. The selector ranks implementation files using AI inventory signals, technical-objective matches, production entry points, imports, callers, and bounded graph relationships. Documentation is not used as the primary proof of an implementation.
5. The provider receives the selected redacted excerpts, relevant graph context, versioned code objectives, and typed declared system facts in one structured request.
6. The model may request one follow-up containing at most three literal search terms and optional repository-relative path hints. Trusted local code—not the model—searches only eligible discovered files and returns at most three bounded excerpts. If useful excerpts are found, ComplyScan makes one final structured request. If the initial model response instead exhausts its output allowance, ComplyScan uses that same sole second-call budget for a terse no-follow-up recovery. There are no further rounds.
7. Trusted code validates objective IDs, configured system IDs, AI-use IDs, classifications, paths, line citations, and ownership boundaries. Invalid output rejects the model pass.
8. The advisory result is saved separately from deterministic findings and does not change the scan's CI threshold.

The normal explicit-review path therefore uses one model call, or at most two when a useful follow-up is requested. It does not create one request per directory or per objective.

## Pull-request review scope

`complyscan review --changed-since <git-ref>` keeps the repository-wide governance layer local while narrowing every source-bearing model request to the change. ComplyScan includes every changed model-eligible source, manifest, configuration, CI, container, infrastructure, or environment-template file. It then uses the complete repository graph locally to add at most eight unchanged source files connected within two call, route, authorization, persistence, logging, configuration, test, or import hops. Unrelated files are never added merely because they contain an AI or compliance keyword.

The same in-memory changed-plus-connected repository is the hard boundary for targeted selection, broad review modes, per-objective technical investigations, eligible-file manifests, and model-requested follow-up searches. Consequently, choosing `deep`, `full`, or `hierarchical` together with `--changed-since` can broaden analysis only inside that bounded change context; it cannot restore whole-repository model access. Deterministic inventory, framework evidence, reconciliation, documentation checks, and risk-file checks still use the complete local snapshot as documented by their own scan scopes.

Terminal, Markdown, and JSON coverage distinguish the full locally checked repository from the model boundary. They record the `changed-plus-connected` review scope, changed files included, connected files included, files available to local context selection, and excerpts actually submitted. Files outside that boundary were not reviewed by the model, so an absent model observation is never evidence that an unchanged implementation is missing.

## Modes

Configure the strategy under `ai.repository-analysis`:

```yaml
ai:
  repository-analysis:
    mode: auto
    max-input-tokens: 180000
```

- `auto` is the recommended default and currently selects targeted evidence.
- `targeted` explicitly selects the same targeted-evidence strategy as `auto`.
- `deep` enables the previous broad strategy: one full request when it fits, otherwise hierarchical subsystem analysis and synthesis.
- `full` requires one broad request and reports an incomplete model layer when the configured budget is too small.
- `hierarchical` always analyzes broad subsystem slices and synthesizes them.
- `bounded-only` disables repository-level analysis and retains the older per-candidate technical investigation flow.

`max-input-tokens` remains the upper safety budget for broad modes. Targeted remote analysis additionally aims for a compact package of about 6,500 input tokens. For OpenAI GPT-5.6, both the initial request and any output recovery use medium reasoning effort with low text verbosity. The initial 4,096-token output allowance is a bootstrap value, not the model maximum: ComplyScan cannot know an account's token-per-minute ceiling before a response arrives. If the response exhausts that allowance, ComplyScan reads the OpenAI account and project token-limit headers and makes its sole recovery request with the maximum usable output allowance: `min(128000, returned token ceiling - actual input tokens)`. If OpenAI does not return a usable ceiling, ComplyScan keeps the 4,096-token allowance rather than risk a guaranteed 429. Experimental Ollama analysis uses a larger local package because no source leaves the machine. Input budgets are conservative estimates, not exact provider tokenization or billing guarantees.

The JSON evidence bundle records the actual mode as `targeted-evidence`, `full-repository`, or `hierarchical-synthesis`, together with discovered and submitted coverage, checked citations, token usage where supplied, and whether a bounded follow-up was used.

## What is sent by default

Targeted analysis may send selected excerpts from source code, dependency manifests, configuration, CI, GitHub Actions, Docker, Terraform, and environment templates. Selection is grounded in scanner matches and code structure. It does not send every eligible file, and it does not treat an unselected file as evidence that an implementation is absent.

The raw active ComplyScan configuration—normally `.complyscan.yml`, or the exact file passed with `--config`—is never part of the discovered snapshot or a model request. ComplyScan separately constructs typed system facts from that configuration and supplies only fields needed to interpret technical objectives. Other YAML files, including `.github/workflows/*.yml`, remain eligible because they can implement CI, permissions, tests, deployment, or safeguards.

Generated outputs, dependency trees, build directories, caches, binaries, symlinks, ignored paths, nested repositories unless explicitly enabled, files over the discovery limit, and credentials matching maintained secret patterns are not sent. Secret redaction is defence in depth, not a guarantee that arbitrary proprietary or personal data has been removed. A hosted provider remains an external source-code processing boundary.

The explicit `deep`, `full`, and `hierarchical` modes can send substantially more eligible repository text. Use them only when the extra coverage, provider limits, cost, and organisation policy have been considered.

## Rate limits and retries

Compact targeted packages and bounded output schemas are intended to fit ordinary provider limits rather than consume a limit through many subsystem calls. Targeted responses cap AI uses, objective observations, unmapped activity, citations, unresolved questions, and individual text fields. Reports and JSON distinguish input, total output, and reasoning tokens where the provider supplies that breakdown. If a hosted provider still reports a temporary rolling rate limit, ComplyScan honours the provider interval, waits at least one minute, shows a cancellable countdown, and retries within the existing per-request cumulative wait budget. An intrinsically oversized targeted request is not turned into an unbounded directory sweep.

Deep modes retain adaptive splitting, incomplete-response recovery, hierarchical synthesis, and rate-limit waiting. These modes can require many requests and should not be the normal developer workflow.

## What the result means

An AI-use entry is a review candidate supported by submitted repository citations. An objective observation says how strongly the submitted evidence supports a code objective. An unmapped observation records detected AI activity that does not fit a supplied objective. None establishes legal applicability, an organisation's statutory role, actual deployment, complete runtime behaviour, human practice, operational effectiveness, or compliance.

No AI use identified is not proof that a repository has no AI. `not_supported` is not proof that a mechanism is absent. A checked citation proves only that the referenced discovered path and line exist; a reviewer must still decide whether the model's interpretation is correct.

## Current limitations

- Targeted selection can miss implementations that produce no inventory signal, objective match, production-entrypoint relationship, or supported graph relationship.
- Static graph coverage is strongest for Go, Python, JavaScript, and TypeScript and remains incomplete for dynamic dispatch, runtime registration, metaprogramming, and other languages.
- Repository analysis is advisory and is not merged into deterministic CI failure thresholds.
- Completed repository results are reused only for an exact private-cache identity covering active review-scope content, framework evidence, profiles, ownership, provider endpoint, model, prompt contract, context strategy, and token budget. A full review binds the full discovered repository; `review --changed-since` binds the changed-plus-connected model scope defined above. Any changed identity input causes a new request; `--refresh-review` always bypasses reuse.
- Context size and cost depend on the selected provider, model, and repository. ComplyScan records usage supplied by the provider but does not estimate price because provider prices change independently of the CLI.
