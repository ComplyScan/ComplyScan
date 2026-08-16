# Automatic model qualification

ComplyScan's explicit configuration and automation interfaces accept arbitrary exact Ollama tags and model IDs supported by its native or OpenAI-compatible hosted adapters. Standard interactive setup intentionally exposes only a small cloud shortlist; everything outside that list is an experimental configuration and receives no maintained quality claim.

Before an unseen model receives repository context, ComplyScan sends one small synthetic record through the configured provider. The record contains no repository data and includes instruction-shaped untrusted text. A model is marked **compatible** only when it returns the required structured object, preserves the trusted record binding, and does not create an extra record from the untrusted instruction.

Interactive setup runs this check automatically after the selected local model is installed or the selected remote credential is available. A later `complyscan scan` also runs it when `ai.review-on-scan: true` has matching private trust on that machine and the configured model has no valid cached result. The trust identity binds provider, endpoint, model, and credential environment-variable name. Repository intent alone—such as a freshly cloned configuration—does not trigger qualification or source transfer. Explicit provider/model/endpoint/refresh/deep/review options provide one-run consent. `complyscan scan --deterministic-only` never performs qualification or contacts a provider. Non-interactive setup does not contact a provider unless `--qualify-model` is explicitly supplied; remote qualification may incur a small provider charge.

Successful results are cached under the operating system's private user-cache directory for 30 days. The cache key binds:

- provider and exact model ID;
- the Ollama model digest when the local service exposes it, or a hash of the configured compatible endpoint; and
- the finding-review, profile-draft, and technical-review prompt contract versions.

A changed model, digest, or prompt contract therefore requires a new check. Failed checks are not cached because availability, credentials, rate limits, and provider behavior can recover. The cache contains only identity, timestamps, token counts, and the fixed compatibility description; it contains no credential, synthetic response text, repository source, or human profile answer.

Use the configured model's cached status with:

```sh
complyscan doctor .
```

Force a fresh check with:

```sh
complyscan doctor --probe-review .
```

An ordinary `doctor` run treats a missing optional API key, Ollama service, or Ollama model as a warning because deterministic scans remain available and a matching private review cache may still be reusable. `--probe-review` explicitly requires a live provider dependency; if one is missing, `doctor` reports a blocking failure and skips the compatibility request rather than attempting a call that cannot succeed.

If qualification fails during setup, setup continues with deterministic suggestions and human questions. If it fails during a trusted or explicitly requested scan, ComplyScan preserves the deterministic report and skips repository submission to that model. `--require-ai-review` makes an absent or incomplete AI layer return exit code 2 after the report is saved, but never grants processing consent by itself.

## Compatibility is not validation

The automatic check deliberately answers only: “Can this provider/model obey ComplyScan's minimum structured and binding contract right now?” It does not measure general reasoning quality, evidence precision or recall, legal accuracy, latency across realistic workloads, or suitability for a particular repository.

ComplyScan uses separate labels:

- **compatible** — passed the automatic synthetic contract;
- **validated for setup drafting** — passed the maintained labelled questionnaire-draft corpus;
- **validated for technical review** — passed every maintained adversarial technical-review fixture; and
- **experimental/unqualified** — selectable, but no current automatic result exists or the check failed.

Promotion requires both task-specific validations for the exact model and prompt versions. All model output remains advisory and human-confirmed regardless of status. See the [model support policy](model-support-policy.md).
