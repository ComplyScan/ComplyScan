# Security policy

## Supported versions

Security fixes are provided for the latest released version of ComplyScan. The project is currently in the `0.1.x` development line.

## Reporting a vulnerability

Please do not open a public issue for a suspected vulnerability that could expose credentials, source code, or users. Use GitHub's private vulnerability reporting feature for this repository and include:

- the affected version and platform;
- reproduction steps or a minimal safe proof of concept;
- the likely impact; and
- any suggested mitigation.

Do not include real secrets or third-party private source. Maintainers will acknowledge a complete report as soon as practical, investigate it, and coordinate disclosure and a fix with the reporter.

## Security invariants

Every `complyscan scan` starts with the local deterministic boundary. That layer has no telemetry, uploads no source, skips symlinks and oversized or binary content, and applies best-effort redaction for recognised credential formats before evidence is reported. Real dotenv files such as `.env`, `.env.local`, and `.env.production` are excluded by name before their contents are read; explicit templates such as `.env.example`, `.env.sample`, `.env.template`, and `.env.dist` remain eligible repository evidence and are still redacted. `complyscan scan --deterministic-only` guarantees that the process does not contact a model or read provider credentials.

AI processing requires both repository intent and a non-transferable consent boundary:

- `complyscan setup` performs local inspection first, but may contact a selected provider for editable setup suggestions after disclosure and consent. Selecting a provider saves repository intent as `ai.review-on-scan: true` and stores a matching private trust record outside the repository on that machine. Setup may also install or start Ollama and download model weights only after separate confirmation.
- automatic model use requires both that repository intent and private trust matching the provider, endpoint, model, and credential environment-variable name. Cloning a repository, changing that identity, or starting an ephemeral runner cannot silently authorize source transfer. The scan stays deterministic and prints an actionable trust or migration note.
- provider, model, endpoint, refresh, deep, and hidden review options are explicit one-run consent. `--require-ai-review` is a completeness policy only: it never grants consent and, when no trusted layer can run, returns exit code `2` after preserving deterministic reports.
- a trusted or explicitly consented scan runs the deterministic foundation first and then sends one or more bounded, best-effort-redacted candidate packages, followed by synthesis when needed, to the configured loopback or hosted provider. For hosted adapters, independent source batches and independent groups within one synthesis level may run concurrently using both request and token capacity metadata when both are available, or a conservative header-free ramp, up to 32 calls. Ollama remains serial because its local endpoint exposes no portable request/token capacity. Validation repairs and transient or rate-limit retries can resend the same bounded source package and incur cost faster. Automatic recovery is finite: at most two schema repairs, eight transient retry cycles and ten cumulative wait minutes per logical request, with a 256-provider-request repository-analysis ceiling. Permanent quota failures return promptly. These controls preserve deterministic results but do not guarantee a final AI output under permanent provider or schema failure. Hosted processing remains subject to that provider's terms and account settings. A missing key, model, or provider leaves deterministic results intact and marks the AI layer incomplete.
- legacy configurations with a provider but without `ai.review-on-scan: true` keep their previous deterministic scan behavior until setup records repository intent and private trust. The hidden `complyscan review` compatibility command remains an explicit migration path.
- `complyscan doctor --probe-review` sends fixed source-free finding-binding and repository-shaped compatibility requests to the configured provider (normally two, at most four including temporary retries).
- the GitHub Action forces deterministic-only scanning by default through `ai-review: none`, independently of repository intent or private trust. Model processing requires an explicit `ai-review: configured` or provider value; prefer a pinned standard provider and model in a protected workflow. Automatic change scope applies only to `pull_request`; change-scoped `pull_request_target` runs are refused because their default checkout cannot represent proposed changes safely. A target-event workflow may consciously select `scope: full` only for an intended base-branch scan. Treat any target-event checkout and secret exposure as a separate GitHub security boundary. The Action can upload source-excerpt-free SARIF and, by default, append the concise Markdown report to the job summary. Set `upload-results: false` or `publish-summary: false` for repositories whose GitHub access policy does not permit those disclosures; raw JSON is never uploaded automatically.
- the installer and release updater use the network to obtain signed or checksummed project artifacts.

Automatic-review trust records live beneath the operating system's per-user cache in `complyscan/review-consent`, outside the repository. The directory and files require private permissions, reject symlinks and non-regular files, and are read with a size limit and strict schema. A record contains the canonical repository and configuration paths, a digest of the complete AI destination, context settings, the `adaptive-provider-pipeline-v2` context contract, and a timestamp—never an API-key value or repository source. That contract includes provider-neutral source and synthesis concurrency plus bounded retransmission during repair and retry, so approval for an older transfer pattern is not reused. Running setup with AI disabled revokes the record for that repository and configuration identity.

ComplyScan's recognised-secret redaction is defence in depth, not a guarantee that arbitrary credentials, personal data, internal URLs, or proprietary literals have been removed. Remote review must remain opt-in, bounded, documented, cancellable, and covered by tests. Repair and retry counts, cumulative waits, and provider requests must stay bounded and visible in accounting; provider-specific wire-schema normalization must never weaken the complete trusted local validation contract. Any new telemetry, background transfer, broader context selection, endpoint behavior, credential handling, or persistence of submitted source requires explicit security review and documentation.
