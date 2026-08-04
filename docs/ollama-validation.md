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

Generated JSON, resource metrics, and the validation summary are saved under `.complyscan/validation/ollama/` and are ignored by Git. Review the JSON and metrics before recording a qualified release result. Override the defaults with `COMPLYSCAN_OLLAMA_MODEL` or `COMPLYSCAN_VALIDATION_DIR` when needed.
