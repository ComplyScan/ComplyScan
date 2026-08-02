# Architecture

ComplyScan v0.2 has a deliberately small offline pipeline:

```text
target directory
  → bounded discovery, repository boundaries, and ignore evaluation
  → text/binary, file-count, and byte safeguards
  → file classification
  → typed provider and framework signal extraction
  → optional Git changed-file scope
  → independently registered deterministic rules
  → fingerprinting, reasoned suppressions, and baseline filtering
  → terminal, JSON, or SARIF reporter
```

`internal/discovery` owns filesystem traversal and file classification. It returns repository-relative slash-separated paths so findings are stable across macOS, Linux, and Windows. It skips nested repositories by default and supports Git-tracked-only scans, progress callbacks, and explicit scan budgets.

`internal/inventory` extracts typed signals from dependency declarations, source imports, recognised endpoints, and actual environment-variable access. It aggregates those signals into a versioned component report with scope, evidence type, package version, confidence, and location fields. Detection-signature files can carry the narrow `complyscan:ignore-ai-signals` marker so synthetic definitions do not self-identify as runtime components.

`internal/rules` owns the severity and finding models plus the deterministic rule interface. Rules receive a read-only repository snapshot. During ordinary scans every rule sees the full snapshot. During `--changed-since` scans, file-local rules receive only committed, staged, unstaged, and untracked changed files; rules implementing `RepositoryWideRule` retain the full snapshot. The documentation and risk-evidence checks use that interface so a small pull request cannot bypass repository-level governance.

`internal/governance` renders AI-system and risk-assessment scaffolds from structured inventory evidence. Generators preserve discovery warnings, label documents as drafts, require human completion, and protect existing documents unless overwrite is explicit.

`internal/baseline` stores deterministic finding identities without source evidence. Configured suppressions require a review reason; both mechanisms are applied before streaming, reporting, and exit-code evaluation.

`internal/report` constructs a format-neutral finding report. Terminal output streams findings live, while JSON and SARIF 2.1.0 remain buffered for valid deterministic output. SARIF carries source locations and partial fingerprints for code-scanning deduplication. Structured component inventory has its own versioned JSON model in `internal/inventory` because components and compliance-engineering findings are different records.

`internal/providers` defines the future review boundary:

```go
type Provider interface {
    Review(ctx context.Context, request ReviewRequest) (ReviewResult, error)
}
```

Reserved provider kinds are Ollama, OpenAI, Anthropic, Gemini, and ComplyScan Cloud. They are types only: v0.2.0 instantiates none of them, never makes network calls, and always uses deterministic rules. Model-assisted review is deferred to v0.2.1 or later. A future provider layer must be explicit opt-in, preserve secret redaction, minimise source disclosure, and keep enriched observations separate from legal conclusions.
