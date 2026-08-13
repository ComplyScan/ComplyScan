# Repository-wide AI analysis

Repository-wide analysis is an advisory model layer added after deterministic discovery. Its purpose is to reduce the blind spots created when keywords choose the model's context before the model can reason about the repository.

## Pipeline

1. ComplyScan discovers text under the existing `.gitignore`, exclusion, file-size, file-count, total-byte, binary, symlink, dependency-directory, generated-output, and nested-repository boundaries.
2. It classifies files, redacts recognised credential formats, and builds the language-neutral Go, Python, JavaScript, and TypeScript repository graph.
3. It loads the selected versioned code-only framework objectives and the configured system facts.
4. If all relevant context fits the configured input budget, the provider receives it in one request. Otherwise files are grouped by top-level subsystem, every group is analyzed, and structured results are reduced through one or more synthesis levels.
5. The model discovers technically evidenced AI uses, maps repository evidence to only the supplied objectives, and lists activity it could not map.
6. Trusted code validates the response. Unknown objective IDs, unknown configured system IDs, duplicate AI-use IDs, unsupported classifications, invented paths, and out-of-range line citations reject the repository-wide pass.
7. The result is saved separately from deterministic findings and reconciliation. The older bounded finding and technical-objective reviews still run as comparison and fallback layers.

## Context modes

Configure the strategy under `ai.repository-analysis`:

```yaml
ai:
  repository-analysis:
    mode: auto
    max-input-tokens: 180000
```

- `auto` sends one request when relevant context fits and uses hierarchical analysis otherwise.
- `full` requires one request and reports an incomplete model layer when the configured budget is too small.
- `hierarchical` always analyzes subsystem slices and synthesizes them.
- `bounded-only` disables repository-wide source transfer and retains the previous finding and candidate investigation flows.
- Omitting `max-input-tokens` uses a conservative default of 180,000 for hosted providers and 24,000 for experimental Ollama. The estimate reserves space for prompts, objectives, system context, the repository graph, and model output. It is a safety budget rather than an exact provider tokenizer.

The JSON report records the actual mode as `full-repository` or `hierarchical-synthesis`, along with discovered repository files/bytes, submitted file occurrences/bytes, subsystem count, verified citations, and provider-reported token usage.

## What is sent

The repository-wide request may contain relevant discovered source, dependency manifests, configuration, CI, GitHub Actions, Docker, Terraform, README, governance, model-card, privacy, and risk text; the selected technical objectives; declared system facts; and report-safe graph symbols, imports, relationships, and reachability. Generic unclassified text is omitted. Source content is secret-redacted again at the provider boundary.

Generated outputs, dependency trees, build directories, caches, binaries, symlinks, ignored paths, nested repositories unless explicitly enabled, files over 1 MiB, files beyond discovery limits, and complete credentials matching maintained secret patterns are not sent. Secret redaction is defence in depth, not a guarantee that arbitrary proprietary or personal data has been removed. A hosted provider remains an external source-code processing boundary.

## What the result means

An AI-use entry is a review candidate supported by repository citations. An objective observation says how strongly the submitted repository supports that code objective. An unmapped observation records detected AI activity that does not fit any supplied objective. None of these establish legal applicability, an organisation's statutory role, actual deployment, complete runtime behavior, human practice, operational effectiveness, or compliance.

No AI use identified is not proof that a repository has no AI. `not_supported` is not proof that a mechanism is absent. A checked citation proves only that the referenced discovered path and line exist; a reviewer must still decide whether the model's interpretation is correct.

## Current limitations

- Hierarchical boundaries follow repository layout and are not inferred AI-system boundaries.
- Static graph coverage is strongest for Go, Python, JavaScript, and TypeScript and remains incomplete for dynamic dispatch and runtime registration.
- Repository-wide output is not yet merged into deterministic reconciliation or CI failure thresholds. It is deliberately non-blocking while comparative quality benchmarks are built.
- Repository-wide analysis is not cached yet. The bounded technical investigation retains its existing source-free cache.
- Context size and cost depend on the selected provider/model and repository. ComplyScan reports provider token usage where available but does not estimate price because provider prices change independently of the CLI.

