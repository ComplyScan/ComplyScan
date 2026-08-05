# Pinned public-repository study

This directory contains provenance and human labels, not third-party source code. The manual benchmark downloads three MIT-licensed public repositories at exact commits and evaluates them with the same deterministic technical-evidence engine used by the CLI.

The repositories were selected to provide a larger Go AI runtime, a compact Python evaluation suite, and a large multi-language AI evaluation and red-team project. `sources.json` records the exact URLs, revisions, licence identifiers, and licence-file paths. `manifest.json` records candidate-level labels and acceptance thresholds for technical evidence only.

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

## Baseline review

For technical pack `0.1.1`, the reviewed deterministic baseline has 12 true-positive candidates, five false-positive candidates, and no false negatives: 70.6% candidate precision and 100% recall across the labelled paths. All eight expected per-repository language detections are present. Context anchors and relationships are not labelled in this external study; those are enforced by the checked-in synthetic benchmark.

The five deliberately unlabelled results include a LocalAI test of model-load log messaging plus Promptfoo documentation or generic-test signals: two interactive website pages about AI safety/security and two source/test references whose nearby words resemble a technical control without implementing that control. They remain visible as false positives instead of being hidden by broad path exclusions, because tests and user-facing source can themselves be valid evidence for other objectives. Optional semantic review is the intended filtering layer.

On 2026-08-04, a complete `qwen3:8b` run reviewed all 17 deterministic candidates and produced 11 true positives, one false positive, four true negatives, and one false negative: 91.7% precision, 91.7% recall, 80% negative specificity, and 100% review coverage. The model missed Promptfoo's medical anchoring-bias rubric and accepted an interactive safety/security quiz. It used 39,379 prompt tokens and 4,338 completion tokens, with about 30.5 minutes of model-reported duration. Model results can vary across runs and hardware; these values demonstrate a passing checked policy, not stable general accuracy or a release SLA.

Both disagreements are now locked into provider regression tests through general transparent guardrails. The recorded metrics remain the historical live result; they are not rewritten from unit-test expectations. Repeat the complete live study to measure the revised prompt and guardrails across multiple model runs.

Only bounded candidate context is sent to the local Ollama model. The JSON result contains sanitized rationales and decision metadata but no source excerpts. These labels say only whether a file is a reasonable candidate for human technical review. They do not state that a repository, product, or control complies with the EU AI Act. Re-review the complete candidate set before changing a pinned revision, pack version, keyword boundary, semantic policy, or threshold.
