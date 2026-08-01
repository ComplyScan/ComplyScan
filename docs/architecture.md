# Architecture

ComplyScan v0.1 has a deliberately small offline pipeline:

```text
target directory
  → discovery and .gitignore evaluation
  → text/binary and size safeguards
  → file classification
  → independently registered deterministic rules
  → structured findings
  → terminal or JSON reporter
```

`internal/discovery` owns filesystem traversal and file classification. It returns repository-relative slash-separated paths so findings are stable across macOS, Linux, and Windows.

`internal/rules` owns the severity and finding models plus the deterministic rule interface. Rules receive the same read-only repository snapshot. The scanner only iterates enabled rules and sorts their results; adding a rule does not add rule-specific branches to scanner core.

`internal/report` constructs a format-neutral report. Terminal and JSON are v0.1 renderers; SARIF can be added as another renderer.

`internal/providers` defines the future review boundary:

```go
type Provider interface {
    Review(ctx context.Context, request ReviewRequest) (ReviewResult, error)
}
```

Reserved provider kinds are Ollama, OpenAI, Anthropic, Gemini, and ComplyScan Cloud. They are types only: v0.1 instantiates none of them, never makes network calls, and always uses deterministic rules. A future provider layer should be explicit opt-in, preserve secret redaction, minimise source disclosure, and keep its enriched observations separate from legal conclusions.
