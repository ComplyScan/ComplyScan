# Contributing

We would like to try something a little different with this repository.

Coding agents can produce a large implementation very quickly. For a new feature or meaningful behaviour change, please start with a short, human-written issue rather than a large pull request. Write it as you would explain the idea to a coworker: what problem did you encounter, what should ComplyScan do instead, and why would that be useful to a developer?

Please do not ask an AI to expand the idea into a formal proposal. A few honest paragraphs are more useful than a polished specification. If we agree on the direction, either you or a coding agent can implement it.

Bug fixes can go directly to a focused pull request. Include a reproduction, a regression test, and a brief explanation of why the fix is safe. Report security vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## Using coding agents

Agent-written implementation is welcome. The contributor remains responsible for understanding and reviewing the change, keeping it focused, and verifying its behaviour. Please mention material agent use in the pull-request description so reviewers understand how the change was produced and tested.

Do not include real credentials, customer data, or sensitive production material in prompts, fixtures, issues, or pull requests.

## What every change must preserve

- Keep deterministic code evidence separate from legal interpretation.
- Prefer conservative, explainable signals with a path, line, confidence, and useful next step.
- Do not add telemetry or background network access. External processing must remain explicit, bounded, consented, documented, and covered by privacy and failure-path tests.
- Treat repository AI configuration as intent, not transferable consent. A new machine or CI runner must establish its own trust or use explicit one-run provider selection.
- New rules need a stable ID plus positive, negative, and edge-case tests.
- Framework-pack changes must cite an authoritative primary source and include only requirements that repository evidence can meaningfully support. Summarise legal text; do not copy protected standards without redistribution rights.
- Changes to technical-evidence matching or indexing must update the affected labelled fixtures in `testdata/technical-evaluation`. Do not lower an acceptance threshold merely to make a regression pass.

## Before opening a pull request

Run:

```bash
go test ./...
go vet ./...
```

For technical-evidence changes, also run:

```bash
./scripts/evaluate-technical-evidence.sh
./scripts/evaluate-technical-evidence.sh --manifest testdata/technical-evaluation/nist-manifest.json
```

Keep the pull request small and explain its privacy impact, false-positive considerations, and validation. By participating, you agree to follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Contributions are accepted under the Apache License 2.0.
