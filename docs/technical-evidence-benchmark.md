# Technical evidence benchmark

ComplyScan maintains deterministic benchmarks for the shared code-only control layer used by its framework packs. Separate EU AI Act and NIST AI RMF manifests label source-specific objectives while shared control IDs protect common matcher behavior. The benchmarks catch scanner and repository-graph regressions before they reach users. They do not measure legal compliance, a complete NIST AI RMF implementation, or general accuracy across arbitrary repositories.

Run the checked-in EU corpus from the repository root:

```sh
./scripts/evaluate-technical-evidence.sh
```

Run the NIST corpus with its pinned manifest:

```sh
./scripts/evaluate-technical-evidence.sh --manifest testdata/technical-evaluation/nist-manifest.json
```

For automation or comparison tooling, request the source-free JSON result:

```sh
./scripts/evaluate-technical-evidence.sh --manifest testdata/technical-evaluation/nist-manifest.json --format json
```

The runner exits `0` when every aggregate acceptance threshold passes, `1` when a quality threshold fails, and `2` for an invalid manifest or operational error. CI runs both manifests on every supported Go version in addition to the normal tests.

## What is measured

The versioned manifests at `testdata/technical-evaluation/manifest.json` and `testdata/technical-evaluation/nist-manifest.json` each bind an exact technical pack ID and version. For each repository case they label:

- every expected objective and repository-relative evidence path;
- the expected enclosing symbol and production, exported, test-only, or unreached classification;
- required graph relationships, optionally including label, endpoints, and resolution state;
- relationships that must not appear; and
- languages that must be indexed.

The result reports candidate precision and recall, anchor accuracy, reachability accuracy, required-relationship recall, forbidden-relationship hits, and language coverage. Per-case diagnostics identify unexpected or missing candidates and incorrect context. Reports contain labels and aggregate metrics only; they do not serialize repository source.

The EU corpus contains repository-shaped Go operations, Python model-pipeline, JavaScript review-service, and TypeScript assistant cases plus a hard-negative repository. It exercises routes, dataset validation, bias and performance tests, audit logging, human review, override and stop mechanisms, prompt-injection filtering, interaction disclosure, and synthetic-content provenance.

The NIST corpus covers every one of the 11 pack objectives. It reuses shared-control cases for dataset validation, human oversight, performance criteria, safe deactivation, security, fairness, and appeal or override. A dedicated Go operations case covers production behavior monitoring, fail-safe testing, incident recovery, and third-party model monitoring. A separate hard-negative repository places tempting keyword groups in different files to ensure the matcher does not combine unrelated fragments across file boundaries. The enforced baseline has 11 true-positive candidates, no false positives, no false negatives, and 100% anchor, reachability, relationship, and language scores. These small maintained cases are regression evidence, not a substitute for larger representative-repository studies.

## Pinned public-repository study

A separate manual study evaluates exact commits of three MIT-licensed public AI repositories without committing their source. Run it with network access:

```sh
./scripts/evaluate-external-repositories.sh
```

The provenance catalog and human candidate labels live under `testdata/technical-evaluation/external`. The runner creates a temporary workspace, fetches only the pinned revisions, verifies each checkout and licence file, scans locally, prints source-free metrics, and deletes the checkout. Use `--workspace DIRECTORY` to reuse existing pinned checkouts and `--format json` for automation.

The EU pack `0.1.3` baseline records 12 true-positive candidates, five false positives, no false negatives, and complete expected language detection: 70.6% precision and 100% recall on the labelled paths. Version 0.1.3 introduces canonical control IDs without changing the measured EU evidence-match terms. The remaining false positives are retained and documented because this deterministic retrieval stage is designed to favor reviewable candidates; optional semantic review can reject them. This small public-repository study is evidence for regression tuning, not a general accuracy claim, and it is not a network-dependent CI gate. The NIST pack still needs separately reviewed public-repository labels before any NIST-specific real-world accuracy claim.

With Ollama running and `qwen3:8b` installed, add `--review ollama` to evaluate every deterministic candidate using the policy and thresholds in `testdata/technical-evaluation/external/semantic.json`. ComplyScan makes a separate schema-constrained request for each candidate and attaches the decision to its trusted objective/fingerprint pair outside the model. The policy rejects only `not_supported`; missing observations are retained but reduce review coverage. On 2026-08-04, the complete 17-candidate run reached 91.7% precision, 91.7% recall, 80% negative specificity, and 100% coverage. Its one false positive and one false negative remain documented in the external study README. Live-model evaluation is manual, slow, and variable, so it does not run in CI and cannot support a general accuracy claim.

The two observed disagreements are now explicit provider regression cases: discussion-only interactive quizzes are rejected, and executable evaluation rubrics are retained for review even when dynamic registration prevents static reachability. These deterministic tests protect the reasoning boundary without rewriting the recorded 2026-08-04 historical live-run metrics. The repeated result below separately measures the revised prompt's variability.

That repeated benchmark completed on 2026-08-05 with three fresh prompt-version-3 runs. Effective ComplyScan decisions were identical across all runs and reached 100% precision, recall, specificity, and coverage over 17 candidates. Raw Qwen decisions were also stable but materially weaker: worst-case and per-run precision was 84.6%, recall 91.7%, and specificity 60%. Seven transparent guardrail adjustments occurred in every run, three of which changed candidate acceptance. Exact final strength and confidence were stable for every candidate; one candidate's rationale wording varied. Model-reported duration ranged from 7.6 to 8.8 minutes. See the source-free aggregate record at `testdata/technical-evaluation/external/semantic-consistency-2026-08-05.json`.

That record validates the earlier candidate-review prompt, not the current prompt-version-10 multi-framework, system-scoped evidence-investigation, bounded-retrieval, and isolated-test interpretation schema. A prompt-version-8 two-target smoke fixture passed the grounded-claim, assurance, retrieval, and malformed-plan fallback contract on 2026-08-06, but neither it nor the historical 17-candidate figures validate version 10 until the live benchmark is rerun.

The distinction between raw inference and the final policy is intentional: this benchmark supports the combined bounded-model-plus-deterministic-guardrail architecture, not a claim that `qwen3:8b` independently achieved perfect classification. Broader representative studies remain required before Ollama review leaves experimental status.

## Adding a case

1. Add a minimal repository-shaped fixture under `testdata/technical-evaluation/repositories` without real secrets, personal data, or third-party copyrighted source.
2. Run the scanner and inspect every technical candidate, anchor, reachability classification, and relevant relationship.
3. Label every candidate in the source-specific manifest. An unlabelled result is treated as a false positive; a missing label is treated as a false negative.
4. Add required relationships only when the source makes them unambiguous. Use forbidden relationships for meaningful hard negatives.
5. Run the benchmark and the full Go test suite.

Intentional scanner changes may require label changes. Acceptance thresholds must not be reduced merely to accommodate a regression; threshold changes require an explicit rationale and review.

## Evaluating other separate repositories

The runner accepts another manifest with `--manifest`. Case paths must remain inside the directory containing that manifest, so a separate evaluation workspace can place a manifest alongside curated public checkouts or authorised private fixtures. The runner is read-only with respect to cases and uses the same bounded discovery and ignore behavior as ComplyScan.

Do not commit private repository source or generated benchmark JSON to this public repository. Record provenance, licence, revision, labelling method, hardware-independent deterministic metrics, and reviewer decisions separately when conducting a larger study.

Live `qwen3:8b` quality remains a separate gate because model output and resource use can vary. Use `scripts/validate-ollama.sh` for that evaluation after deterministic benchmark changes pass.
