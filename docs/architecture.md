# Architecture

ComplyScan v0.2 development has an offline-by-default pipeline:

```text
target directory
  → validated, version-controlled system profiles
  → provisional applicability and high-risk screening
  → built-in versioned control-pack activation
  → bounded discovery, repository boundaries, and ignore evaluation
  → text/binary, file-count, and byte safeguards
  → file classification
  → repository-wide candidate evidence mapping to individual controls
  → typed provider and framework signal extraction
  → optional Git changed-file scope
  → independently registered deterministic rules
  → fingerprinting, reasoned suppressions, and baseline filtering
  → optional advisory Ollama review of bounded finding records
  → terminal, JSON, or SARIF reporter
```

`internal/discovery` owns filesystem traversal and file classification. It returns repository-relative slash-separated paths so findings are stable across macOS, Linux, and Windows. It skips nested repositories by default and supports Git-tracked-only scans, progress callbacks, and explicit scan budgets.

`internal/profile` owns declared system context, human review attribution, and conservative applicability screening. A repository may contain multiple systems. Controlled values make unknown context and spelling errors visible, while factual free-text labels are bounded. Automated EU AI Act scope and possible high-risk signals are reported separately from human decisions and never produce a compliance verdict. The profile is available to CI through `.complyscan.yml`; interactive questions occur only during setup.

`internal/framework` owns the embedded control-pack schema, strict parsing, content digest, activation policy, evidence matcher, and framework-specific reports. The initial pack freezes an explicitly limited mapping of EU AI Act Articles 9–15 for candidate high-risk providers. It uses eligible file kinds and grouped terms to locate candidate evidence without retaining source excerpts. Matches cannot produce `satisfied` or `compliant`; the maximum automated state is `evidence-found`, with semantic, technical, and human verification still declared per objective. Pack content changes require a new version and digest.

`internal/inventory` extracts typed signals from dependency declarations, source imports, recognised endpoints, and actual environment-variable access. It aggregates those signals into a versioned component report with scope, evidence type, package version, confidence, and location fields. Detection-signature files can carry the narrow `complyscan:ignore-ai-signals` marker so synthetic definitions do not self-identify as runtime components.

`internal/rules` owns the severity and finding models plus the deterministic rule interface. Rules receive a read-only repository snapshot. During ordinary scans every rule sees the full snapshot. During `--changed-since` scans, file-local rules receive only committed, staged, unstaged, and untracked changed files; rules implementing `RepositoryWideRule` retain the full snapshot. The documentation and risk-evidence checks use that interface so a small pull request cannot bypass repository-level governance.

`internal/governance` renders AI-system and risk-assessment scaffolds from structured inventory evidence. Generators preserve discovery warnings, label documents as drafts, require human completion, and protect existing documents unless overwrite is explicit.

`internal/baseline` stores deterministic finding identities without source evidence. Configured suppressions require a review reason; both mechanisms are applied before streaming, reporting, and exit-code evaluation.

`internal/report` constructs a format-neutral report. Terminal output streams deterministic findings live, while JSON and SARIF 2.1.0 remain buffered for valid deterministic output. Terminal and JSON can include profile applicability without changing finding summaries or exit status. SARIF carries source locations and partial fingerprints for code-scanning deduplication; applicability is omitted because it is not a source-located code-scanning result. Optional advisory observations are attached by existing finding fingerprint and remain separate from rule results and summaries. Structured component inventory has its own versioned JSON model in `internal/inventory` because components and compliance-engineering findings are different records.

`internal/providers` defines the optional review boundary:

```go
type Provider interface {
    Review(ctx context.Context, request ReviewRequest) (ReviewResult, error)
}
```

Ollama is implemented as an explicit opt-in provider. It receives only visible, unsuppressed, bounded deterministic finding records—not repository files—and calls a validated loopback `/api/chat` endpoint with non-streaming JSON-schema output. Proxies and redirects are disabled, requests and responses are redacted and bounded, response fingerprints and rule IDs must match submitted findings, and a timeout limits review. Model observations cannot alter deterministic results or exit status.

Framework evidence is not sent across the provider boundary. Future semantic control review must define a separate, explicitly consented request contract with bounded excerpts, prompt-injection controls, secret redaction, identifier binding, and no authority to confirm legal compliance.

OpenAI, Anthropic, Gemini, and ComplyScan Cloud remain reserved types only. Any future remote provider must add an explicit disclosure and consent boundary, preserve secret redaction, minimise source disclosure, and keep model observations separate from legal conclusions.
