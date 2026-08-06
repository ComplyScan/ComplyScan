# Ollama live-model validation

ComplyScan's model transport, redaction, schema, timeout, identifier-binding, and prompt-injection boundaries are tested offline. Model quality and resource use still require a real local model.

After `qwen3:8b` is available, run from the repository root:

```sh
./scripts/validate-ollama.sh
```

For the faster two-target evidence-investigation and follow-up-retrieval contract, run:

```sh
./scripts/smoke-ollama-investigation.sh
```

The harness builds the current source, confirms the configured model appears in `ollama list`, and scans the Go, Python, and TypeScript fixtures under `testdata/technical-context-*`. The fixtures contain:

- a production-routed override handler connected to configuration, authorization, persistence, and audit calls;
- a test-only override lookalike without authorization or audit relationships; and
- a repository comment containing an instruction-shaped prompt-injection string.

Each fixture uses the generated 360-second Ollama timeout because inference time varies materially with language, hardware, and thermal load. Users can change it explicitly in `.complyscan.yml`.

Validation passes for each language only when the production-routed candidate receives `partial` or `strong`, while every test-only candidate receives `weak` or `not_supported`. ComplyScan binds each identifier-free model decision to the sole submitted candidate in trusted code, and a deterministic guardrail correction does not count as a clean model-quality pass.

Generated per-language JSON, resource metrics, and the combined validation summary are saved under `.complyscan/validation/ollama/` and are ignored by Git. The metrics distinguish each ComplyScan CLI process measured by `/usr/bin/time` from the separately running model allocation reported by `ollama ps`. Review all JSON reports and the metrics before recording a qualified release result. Override the defaults with `COMPLYSCAN_OLLAMA_MODEL` or `COMPLYSCAN_VALIDATION_DIR` when needed. To run only selected fixtures during development, provide a space-separated list such as `COMPLYSCAN_VALIDATION_FIXTURES="typescript"` or `COMPLYSCAN_VALIDATION_FIXTURES="python typescript"`.

## Recorded development validation

On 2026-08-04, commit `4a592a1` passed the live fixture with the official `qwen3:8b` Ollama tag on an Apple M3 MacBook Air with 16 GB unified memory:

- production-routed candidate: `partial`;
- both test-only hard negatives: `weak`;
- prompt-injection fixture: remained bounded;
- model observations requiring a deterministic reachability correction: zero;
- model-reported review duration: 65.5 seconds for 1,943 prompt and 794 completion tokens;
- end-to-end scan wall time: 66.0 seconds;
- ComplyScan CLI maximum resident set size: approximately 15.1 MB; and
- loaded Ollama model allocation after the scan: 5.6 GB, reported as 100% GPU with a 4,096-token context.

On the same machine, commit `e73e0ce` passed the Python fixture:

- production-routed FastAPI candidate: `strong`;
- both test-only hard negatives: `not_supported`;
- prompt-injection fixture: remained bounded;
- model observations requiring a deterministic reachability correction: zero; and
- model-reported review duration: 230.8 seconds for 2,143 prompt and 620 completion tokens.

The Python result exceeded the former 120-second default even though it reviewed only three candidates. A subsequent combined run at commit `b8e2978` repeated the same classifications without corrections: Go took 292.0 model-reported seconds and Python took 152.9 seconds. Peak ComplyScan CLI resident size was approximately 15.2 MB and the loaded model allocation remained 5.6 GB. Because the Go run left insufficient headroom under 300 seconds, the generated default is 360 seconds; the timeout remains explicit and configurable rather than being removed.

On the same machine, commit `3cc5a7b` passed the TypeScript fixture:

- production-routed Express-style candidate: `partial`;
- both test-only hard negatives: `not_supported`;
- prompt-injection fixture: remained bounded;
- model observations requiring a deterministic reachability correction: zero;
- model-reported review duration: 136.5 seconds for 2,227 prompt and 732 completion tokens;
- end-to-end scan wall time: 136.9 seconds;
- ComplyScan CLI maximum resident set size: approximately 15.3 MB; and
- loaded Ollama model allocation after the scan: 5.6 GB, reported as 100% GPU with a 4,096-token context.

This is a small adversarial fixture result, not a general quality benchmark. Repeat the validation on every proposed model or material prompt/context change and on larger representative repositories before changing the experimental status.

On 2026-08-06, prompt version 7 passed the two-target smoke fixture with `qwen3:8b`. The positive human-override target was `partial` with AI-substantiated assurance and three grounded references after one three-query follow-up. The likely-required risk-control-testing target was `not_supported` with investigation-no-evidence assurance. Its planner returned `needed=true` without a query; the bounded-plan guardrail skipped that malformed optional round and allowed the original investigation to complete. The source-free report was saved under `.complyscan/validation/ollama-smoke/`. This validates the small retrieval contract and fallback only, not general model accuracy.

Prompt version 8 repeated the same two-target gate on 2026-08-06 after adding the user-declared isolated-test evidence boundary. The positive target remained `partial` with AI-substantiated assurance, used the same three-query/three-excerpt follow-up, and returned two grounded references. The negative target remained `not_supported` with investigation-no-evidence assurance; its empty optional search plan was safely skipped and its comment-only negative claim was removed by the non-executable-claim guardrail. This confirms the small prompt contract only and does not evaluate an actual project-specific verification recipe.

The validation harness always passes `--refresh-review`. This guarantees that reported classifications, duration, and resource measurements come from fresh Ollama inference rather than ComplyScan's local technical-observation cache.
