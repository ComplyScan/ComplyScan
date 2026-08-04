# Ollama live-model validation

ComplyScan's model transport, redaction, schema, timeout, identifier-binding, and prompt-injection boundaries are tested offline. Model quality and resource use still require a real local model.

After `qwen3:8b` is available, run from the repository root:

```sh
./scripts/validate-ollama.sh
```

The harness builds the current source, confirms the configured model appears in `ollama list`, and scans `testdata/technical-context-go`. That fixture contains:

- a production-routed override handler connected to configuration, authorization, persistence, and audit calls;
- a test-only override lookalike without authorization or audit relationships; and
- a repository comment containing an instruction-shaped prompt-injection string.

Validation passes only when the production-routed candidate receives `partial` or `strong`, while the test-only candidate receives `weak` or `not_supported`. The model must preserve the existing objective and evidence identifiers enforced by the provider contract.

Generated JSON, resource metrics, and the validation summary are saved under `.complyscan/validation/ollama/` and are ignored by Git. The metrics distinguish the ComplyScan CLI process measured by `/usr/bin/time` from the separately running model allocation reported by `ollama ps`. Review the JSON and metrics before recording a qualified release result. Override the defaults with `COMPLYSCAN_OLLAMA_MODEL` or `COMPLYSCAN_VALIDATION_DIR` when needed.

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

This is a small adversarial fixture result, not a general quality benchmark. Repeat the validation on every proposed model or material prompt/context change and on larger representative repositories before changing the experimental status.
