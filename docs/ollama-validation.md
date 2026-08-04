# Ollama live-model validation

ComplyScan's model transport, redaction, schema, timeout, identifier-binding, and prompt-injection boundaries are tested offline. Model quality and resource use still require a real local model.

After `qwen3:8b` is available, run from the repository root:

```sh
./scripts/validate-ollama.sh
```

The harness builds the current source, confirms the configured model appears in `ollama list`, and scans both `testdata/technical-context-go` and `testdata/technical-context-python`. The fixtures contain:

- a production-routed override handler connected to configuration, authorization, persistence, and audit calls;
- a test-only override lookalike without authorization or audit relationships; and
- a repository comment containing an instruction-shaped prompt-injection string.

Each fixture uses the generated 360-second Ollama timeout because inference time varies materially with language, hardware, and thermal load. Users can change it explicitly in `.complyscan.yml`.

Validation passes for each language only when the production-routed candidate receives `partial` or `strong`, while every test-only candidate receives `weak` or `not_supported`. The model must preserve the existing objective and evidence identifiers enforced by the provider contract, and a deterministic guardrail correction does not count as a clean model-quality pass.

Generated per-language JSON, resource metrics, and the combined validation summary are saved under `.complyscan/validation/ollama/` and are ignored by Git. The metrics distinguish each ComplyScan CLI process measured by `/usr/bin/time` from the separately running model allocation reported by `ollama ps`. Review both JSON reports and the metrics before recording a qualified release result. Override the defaults with `COMPLYSCAN_OLLAMA_MODEL` or `COMPLYSCAN_VALIDATION_DIR` when needed.

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

This is a small adversarial fixture result, not a general quality benchmark. Repeat the validation on every proposed model or material prompt/context change and on larger representative repositories before changing the experimental status.
