# Architecture

ComplyScan v0.2 has a deliberately small offline pipeline:

```text
target directory
  → bounded discovery, repository boundaries, and ignore evaluation
  → text/binary, file-count, and byte safeguards
  → file classification
  → cached repository-wide technical analysis
  → independently registered deterministic rules
  → fingerprinting, reasoned suppressions, and baseline filtering
  → terminal, JSON, or SARIF reporter
```

`internal/discovery` owns filesystem traversal and file classification. It returns repository-relative slash-separated paths so findings are stable across macOS, Linux, and Windows. It skips nested repositories by default and supports Git-tracked-only scans, progress callbacks, and explicit scan budgets.

`internal/rules` owns the severity and finding models plus the deterministic rule interface. Rules receive the same read-only repository snapshot. The scanner only iterates enabled rules and sorts their results; adding a rule does not add rule-specific branches to scanner core.

`internal/baseline` stores deterministic finding identities without source evidence. Configured suppressions require a review reason; both mechanisms are applied before streaming, reporting, and exit-code evaluation.

`internal/report` constructs a format-neutral report. Terminal output streams findings live, while JSON and SARIF 2.1.0 remain buffered for valid deterministic output. SARIF carries source locations and partial fingerprints for code-scanning deduplication.

`internal/providers` defines the future review boundary:

```go
type Provider interface {
    Review(ctx context.Context, request ReviewRequest) (ReviewResult, error)
}
```

Reserved provider kinds are Ollama, OpenAI, Anthropic, Gemini, and ComplyScan Cloud. They are types only: v0.2 instantiates none of them, never makes network calls, and always uses deterministic rules. A future provider layer should be explicit opt-in, preserve secret redaction, minimise source disclosure, and keep its enriched observations separate from legal conclusions.
