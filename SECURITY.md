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

`complyscan scan` is the local deterministic boundary. It has no telemetry, does not contact a model or read provider credentials, uploads no source, skips symlinks and oversized or binary content, and applies best-effort redaction for recognised credential formats before evidence is reported.

Other commands have separate explicit boundaries:

- `complyscan setup` performs local inspection first, but may contact a selected provider for editable setup suggestions after disclosure and consent. It may also install or start Ollama and download model weights only after separate confirmation.
- `complyscan review` runs the deterministic foundation and then explicitly sends a bounded, best-effort-redacted evidence package to the configured loopback or hosted provider. Hosted processing may incur cost and remains subject to that provider's terms and account settings.
- `complyscan doctor --probe-review` sends a fixed source-free compatibility request to the configured provider.
- the installer and release updater use the network to obtain signed or checksummed project artifacts.

ComplyScan's recognised-secret redaction is defence in depth, not a guarantee that arbitrary credentials, personal data, internal URLs, or proprietary literals have been removed. Remote review must remain opt-in, bounded, documented, cancellable, and covered by tests. Any new telemetry, background transfer, broader context selection, endpoint behavior, credential handling, or persistence of submitted source requires explicit security review and documentation.
