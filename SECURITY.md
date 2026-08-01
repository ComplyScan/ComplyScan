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

The v0.1 scanner is local-only, has no telemetry, performs no network calls, uploads no source, skips symlinks and oversized/binary content, and redacts complete detected credentials from all evidence. A change that affects any of these properties requires explicit security review and documentation.
