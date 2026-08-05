# Pinned public-repository study

This directory contains provenance and human labels, not third-party source code. The manual benchmark downloads ten permissively licensed public repositories at exact commits and evaluates them with the same deterministic technical-evidence engine used by the CLI.

The repositories cover AI runtimes, agent frameworks, orchestration libraries, evaluation suites, guardrails, and red-team tooling across Go, Python, JavaScript, and TypeScript. `sources.json` records the exact URLs, revisions, licence identifiers, and licence-file paths. `manifest.json` records candidate-level labels and acceptance thresholds for technical evidence only.

Run the study with network access:

```sh
./scripts/evaluate-external-repositories.sh
```

To reuse already checked-out pinned repositories, place them in one directory using the IDs from `sources.json` and run:

```sh
./scripts/evaluate-external-repositories.sh --workspace /path/to/checkouts
```

Add `--format json` for a source-free machine-readable result. The runner verifies every checkout's exact commit and licence-file presence before scanning. It exits `0` when thresholds pass, `1` for a metric failure, and `2` for provenance, checkout, or execution errors. The networked study is deliberately not a CI requirement.

To evaluate the deterministic candidates with the configured local model, start Ollama, ensure `qwen3:8b` is installed, and run:

```sh
./scripts/evaluate-external-repositories.sh --review ollama
```

The semantic policy in `semantic.json` sends one candidate per request, rejects only `not_supported`, and conservatively retains a candidate if an observation is missing while failing the coverage threshold. Each request has a 300-second timeout. Use `--ollama-model`, `--ollama-endpoint`, or `--semantic-config` for deliberate experiments. `--semantic-path PATH` isolates one exact candidate for debugging and is not a complete baseline run.

## Initial three-repository baseline

For technical pack `0.1.1`, the reviewed deterministic baseline has 12 true-positive candidates, five false-positive candidates, and no false negatives: 70.6% candidate precision and 100% recall across the labelled paths. All eight expected per-repository language detections are present. Context anchors and relationships are not labelled in this external study; those are enforced by the checked-in synthetic benchmark.

The five deliberately unlabelled results include a LocalAI test of model-load log messaging plus Promptfoo documentation or generic-test signals: two interactive website pages about AI safety/security and two source/test references whose nearby words resemble a technical control without implementing that control. They remain visible as false positives instead of being hidden by broad path exclusions, because tests and user-facing source can themselves be valid evidence for other objectives. Optional semantic review is the intended filtering layer.

On 2026-08-04, a complete `qwen3:8b` run reviewed all 17 deterministic candidates and produced 11 true positives, one false positive, four true negatives, and one false negative: 91.7% precision, 91.7% recall, 80% negative specificity, and 100% review coverage. The model missed Promptfoo's medical anchoring-bias rubric and accepted an interactive safety/security quiz. It used 39,379 prompt tokens and 4,338 completion tokens, with about 30.5 minutes of model-reported duration. Model results can vary across runs and hardware; these values demonstrate a passing checked policy, not stable general accuracy or a release SLA.

Both disagreements are now locked into provider regression tests through general transparent guardrails. The recorded metrics remain the historical live result; they are not rewritten from unit-test expectations. The repeated study below separately measures the revised prompt and guardrails across multiple model runs.

## Repeated semantic consistency

On 2026-08-05, three fresh complete runs used prompt version 3, Ollama model ID `500a1f067a9f`, and commit `c222d66303d3`. All three effective-policy results were identical: 12 true positives, five true negatives, no false positives or false negatives, and 100% precision, recall, negative specificity, and review coverage. Effective acceptance, strength, and confidence were stable for all 17 candidates; exact rationale text was stable for 16. Model-reported duration ranged from 7.6 to 8.8 minutes, with an 8.2-minute mean.

The raw model result must be distinguished from the effective policy. Before deterministic guardrails, every run had 11 true positives, two false positives, three true negatives, and one false negative: 84.6% precision, 91.7% recall, and 60% specificity. Seven strengths were transparently adjusted per run. Four capped test-only evidence without changing candidate acceptance; three changed acceptance by retaining one executable rubric and rejecting two discussion-only quizzes. The checked-in `semantic-consistency-2026-08-05.json` records both layers, usage, variability, platform, model ID, and worst-case values without storing repository source or full rationales.

These repeated results show stable behavior on this exact small corpus. They do not establish general accuracy, remove the experimental label, or eliminate the need for broader repositories and qualified human review.

## Expanded deterministic baseline

On 2026-08-05, the study expanded from three to ten pinned repositories by adding Ollama, OpenAI Agents SDK for Python, LLM Guard, LangGraph, LlamaIndex, NVIDIA garak, and Microsoft PyRIT. The labelled corpus now contains 31 reasonable technical-review candidates. The deterministic engine returned all 31 plus 11 false positives: 73.8% candidate precision, 100% recall, and 100% coverage of the 25 expected repository-language pairs.

The broader review also drove structural filtering for categories that cannot represent application controls: developer-agent skill metadata, tracing-only disable flags, dataset-name selection, dataset-loader tests mistaken for bias evaluation, and compliance terms inside very large embedded parser fixtures. The remaining false positives are preserved for semantic review because their meaning depends on context rather than a generally safe static exclusion.

Only bounded candidate context is sent to the local Ollama model. The JSON result contains sanitized rationales and decision metadata but no source excerpts. These labels say only whether a file is a reasonable candidate for human technical review. They do not state that a repository, product, or control complies with the EU AI Act. Re-review the complete candidate set before changing a pinned revision, pack version, keyword boundary, semantic policy, or threshold.
