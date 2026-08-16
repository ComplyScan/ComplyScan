# Profile-draft quality gate

The profile-draft benchmark measures an experimental compliance-owner assistance capability separately from deterministic scanning and repository review. Ordinary guided setup does not invoke it or present a human questionnaire. Its editable suggestions may be used by a future dashboard or explicit advanced workflow, but they can omit a fact, over-interpret documentation, or cite the wrong place and therefore never become organisation facts automatically.

## Labelled corpus

`testdata/profile-draft-evaluation/manifest.json` defines four small repository-shaped cases:

- a Python support-agent API with required human approval;
- a Python training and evaluation pipeline that explicitly names a personal-data field;
- a public TypeScript synthetic-image API; and
- a documentation-only hard negative containing tempting compliance and AI terminology but no implementation.

The expected labels cover controlled, repository-evident setup fields such as AI activities, deployment model, oversight, decision impact, and explicit data use. The corpus intentionally does not score free-form purpose or user descriptions. Jurisdiction, organisation role, actual production status, legal applicability, legal risk class, and unsupported negative data claims remain outside the model's allowed inference boundary.

The evaluator runs each fixture through production discovery, inventory, and the exact bounded context selector used by setup. It then measures:

- **precision** — how many scored suggestions were expected;
- **recall** — how many expected suggestions were returned;
- **forbidden claims** — suggestions the case explicitly prohibits; and
- **ungrounded references** — citations to a missing file, invalid line, or path not accepted by the label.

The current aggregate acceptance policy requires at least 85% precision and 65% recall, zero forbidden claims, zero ungrounded references, and no case over 300 seconds. These thresholds are a small regression contract, not evidence of general model accuracy or legal reliability. Every suggestion remains editable and requires human confirmation.

## Run the gate

Install and start Ollama, make the candidate model available, then run from the repository root:

```sh
./scripts/validate-profile-draft.sh
```

The default is `qwen3.5:9b`. A quick development run can select one case:

```sh
COMPLYSCAN_PROFILE_DRAFT_CASES="documentation-hard-negative" ./scripts/validate-profile-draft.sh
```

Multiple case IDs are space separated. `COMPLYSCAN_OLLAMA_MODEL`, `COMPLYSCAN_OLLAMA_ENDPOINT`, `COMPLYSCAN_PROFILE_DRAFT_MANIFEST`, and `COMPLYSCAN_VALIDATION_DIR` override the other defaults.

The harness builds the current evaluator and stores a complete JSON result, human summary, and `/usr/bin/time` plus `ollama ps` resource measurements under `.complyscan/validation/profile-draft/`. That directory is ignored by Git. A complete default run can take up to 20 minutes because each of four independent cases has a five-minute ceiling. Failed or timed-out cases are retained in the result and fail the gate.

Offline Go tests exercise manifest validation, production context construction, metric calculation, hard-negative handling, grounded citations, case selection, and the provider's schema and guardrails without starting or downloading a model. Live results are specific to the exact model tag, prompt version, source revision, hardware, and run conditions; rerun the gate for every proposed default model or material profile-draft prompt/context change.

## Recorded development validation

On 2026-08-10, commit `265995b` and profile-draft prompt version 4 passed the complete gate with the official `qwen3.5:9b` Ollama tag on an Apple M3 MacBook Air with 16 GB unified memory:

- 15 true positives, two false positives, and two false negatives;
- 88.2% precision and 88.2% recall;
- zero forbidden claims and zero ungrounded references;
- all four cases completed, with a 157.1-second slowest case and 381.6-second total evaluator wall time;
- 4,132 prompt tokens and 2,781 completion tokens;
- approximately 12.4 MiB peak evaluator resident size; and
- a 5.6 GB loaded Ollama model allocation at a 4,096-token context.

The two false negatives were missed `api` deployment values in the support-agent and synthetic-image cases. The two false positives were `advisory` decision impact and `required` human oversight on the synthetic-image API; neither was supported strongly enough by that fixture. The setup UI still presents every returned value as an editable draft requiring human confirmation. The documentation-only hard negative returned no scored claims.

The ignored result, metrics, and summary were saved locally with timestamp `20260810T202946Z`. This passes the small onboarding regression contract only. It does not validate model-assisted technical-evidence review, every repository shape, every provider, or legal correctness; the separate `qwen3.5:9b` technical-review gate remains pending.
