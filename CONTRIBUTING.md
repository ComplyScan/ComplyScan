# Contributing to ComplyScan

Thank you for helping make AI engineering review more practical.

## Before opening a change

- Search existing issues and explain the technical risk or evidence gap the change addresses.
- Keep deterministic detection separate from legal interpretation.
- Prefer conservative signals with an explainable path, line, confidence, and remediation.
- Do not add telemetry, background network access, or source upload behavior.
- Never add real credentials or sensitive production material to fixtures.

## Development workflow

1. Install Go 1.22 or newer.
2. Create a focused branch.
3. Add or update tests and small `testdata` fixtures.
4. Run `go test ./...` and `go vet ./...`.
5. Open a pull request describing false-positive considerations and privacy impact.

New rules should use a stable ID, implement `rules.Rule`, emit structured findings, redact evidence, and be registered in `rules.DefaultRules`. Include positive, negative, and edge-case tests.

Technical-pack changes must cite an authoritative primary source, contain only objectives that can be evidenced from code, configuration, tests, CI, containers, or infrastructure, preserve visible limitations, and include positive and hard-negative evidence tests. Documentary, organisational, operational, and attestation objectives belong in the future dashboard catalog. Any content change requires a new pack version; reports also bind the exact YAML with a SHA-256 digest. Summarise requirements rather than copying long legal passages. Do not add protected standards such as ISO publications without documented redistribution and machine-processing rights.

By participating, you agree to follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Contributions are accepted under the Apache License 2.0.
