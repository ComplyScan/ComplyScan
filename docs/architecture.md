# Architecture

ComplyScan v0.1.2 has an offline-by-default pipeline:

```text
target directory
  → validated, version-controlled system profiles
  → provisional applicability and high-risk screening
  → bounded discovery, repository boundaries, and ignore evaluation
  → text/binary, file-count, and byte safeguards
  → file classification
  → language-neutral relationship graph with Go, Python, JavaScript, and TypeScript symbol indexing
  → repository-wide code-only technical-objective matching and bounded structural context
  → typed provider and framework signal extraction
  → optional Git changed-file scope
  → independently registered deterministic rules
  → fingerprinting, reasoned suppressions, and baseline filtering
  → optional, separate advisory Ollama reviews of findings and technical candidates
  → terminal output plus atomic Markdown and JSON reports, or SARIF
```

Installation and onboarding are a separate pre-scan path: the POSIX installer selects a release archive for the host platform, verifies it against `checksums.txt`, atomically places the binary in the selected user directory, and invokes `complyscan setup` only when a terminal is present. The setup command reuses `internal/profile` for factual and attributable applicability collection, preserves existing repository configuration, and keeps external provisioning behind individual confirmations. It may invoke Homebrew or Ollama's official Linux installer, start a Homebrew Ollama service, and run `ollama pull`; none of these operations occur during a normal scan.

`internal/discovery` owns filesystem traversal and file classification. It returns repository-relative slash-separated paths so findings are stable across macOS, Linux, and Windows. It skips nested repositories by default and supports Git-tracked-only scans, progress callbacks, and explicit scan budgets.

`internal/profile` owns declared system context, human review attribution, and conservative applicability screening. A repository may contain multiple systems. Controlled values make unknown context and spelling errors visible, while factual free-text labels are bounded. Automated EU AI Act scope and possible high-risk signals are reported separately from human decisions and never produce a compliance verdict. The profile is available to CI through `.complyscan.yml`; interactive questions occur only during setup.

`internal/framework` owns the embedded technical-pack schema, strict parsing, content digest, code-evidence matcher, and objective-specific reports. The CLI pack contains only objectives that inspect source, configuration, tests, CI, containers, dependency declarations, or infrastructure. It records Article references for traceability but does not activate legal controls, inspect uploaded documents, or roll objectives into a compliance status. The first-version matcher requires an eligible file kind, a configured path signal, and every grouped content signal. Matches are `candidate-evidence`; an unmatched check is `not-detected`, never proof that an implementation is missing. Definition files can carry the narrow `complyscan:ignore-technical-evidence` marker to avoid self-identification. Pack content changes require a new version and digest.

`internal/codegraph` owns a language-neutral symbol, import, edge, and reachability model. Go is parsed with the standard library. The dependency-free Python indexer conservatively recognizes indentation-bounded functions, classes, methods, imports, calls, `__main__` entry points, tests, FastAPI and Flask decorators, Django route registrations, dependency-based authorization, configuration access, persistence, and audit/logging calls. The dependency-free JavaScript and TypeScript indexer recognizes functions, arrows, classes, methods, interfaces/types, ESM and CommonJS imports, calls, top-level entry calls, test callbacks, Express/Fastify registrations, Next.js route exports, NestJS decorators and guards, environment access, persistence, and logging. Comments, strings, and template literals are masked before structural extraction. Production entry points and test entry points are traversed separately so test-only or unreached candidates remain visible. Each technical match receives a maximum two-hop context package with report-safe symbol metadata, imports, relationships, and unresolved questions. Source excerpts remain in unexported graph state and cannot enter graph JSON. Unsupported or unparseable source remains visible as incomplete coverage and causes unmatched source objectives to be `not-evaluated`.

`internal/inventory` extracts typed signals from dependency declarations, source imports, recognised endpoints, and actual environment-variable access. It aggregates those signals into a versioned component report with scope, evidence type, package version, confidence, and location fields. Detection-signature files can carry the narrow `complyscan:ignore-ai-signals` marker so synthetic definitions do not self-identify as runtime components.

`internal/rules` owns the severity and finding models plus the deterministic rule interface. Rules receive a read-only repository snapshot. During ordinary scans every rule sees the full snapshot. During `--changed-since` scans, file-local rules receive only committed, staged, unstaged, and untracked changed files; rules implementing `RepositoryWideRule` retain the full snapshot. The documentation and risk-evidence checks use that interface so a small pull request cannot bypass repository-level governance.

`internal/governance` renders AI-system and risk-assessment scaffolds from structured inventory evidence. Generators preserve discovery warnings, label documents as drafts, require human completion, and protect existing documents unless overwrite is explicit.

`internal/baseline` stores deterministic finding identities without source evidence. Configured suppressions require a review reason; both mechanisms are applied before streaming, reporting, and exit-code evaluation.

`internal/report` constructs a versioned evidence bundle with scan identity, UTC timestamp, tool build, and explicit scope. Terminal output streams deterministic findings live. Every successful scan atomically replaces `.complyscan/reports/latest.md` and `latest.json`; generated reports are excluded from discovery, target-relative output cannot escape the repository, and symlink artifact destinations are refused. Markdown is the human report and JSON is the future dashboard contract. SARIF 2.1.0 remains a separate source-location integration. Finding observations are attached by finding fingerprint; technical observations are attached by objective ID and evidence fingerprint. Both remain separate from deterministic results and summaries. Structured component inventory has its own versioned JSON model because components and compliance-engineering findings are different records.

`internal/providers` defines the optional review boundary:

```go
type Provider interface {
    Review(ctx context.Context, request ReviewRequest) (ReviewResult, error)
}
```

Ollama is implemented as an explicit opt-in provider with two calls. Finding review receives visible, unsuppressed, bounded deterministic records. Technical review receives existing objective candidates plus bounded graph metadata and up to six connected symbol excerpts or one bounded non-source file excerpt. Both call a validated loopback `/api/chat` endpoint with non-streaming JSON-schema output. Proxies and redirects are disabled; input and output are redacted and bounded; code and comments are labelled untrusted; identifiers must exactly match submitted records; and each call has a timeout. Model observations cannot alter deterministic results, objective status, legal applicability, or exit status.

Raw technical excerpts exist only in the local Ollama request and are never copied into the Markdown or JSON evidence bundle. A future dashboard will receive that source-free local JSON bundle by explicit connection. Additional language indexers and iterative context retrieval remain future work; any retrieval loop must stay read-only and bounded.

`internal/framework` also owns the source-free technical-evidence benchmark model. A strict versioned manifest labels complete objective candidates, anchors, reachability, required and forbidden relationships, and language coverage for repository-shaped cases. The maintainer runner discovers each labelled repository through the production discovery path, evaluates the embedded pack, emits text or JSON metrics, and fails only against explicit acceptance thresholds. CI runs this independently from the optional live Ollama gate; deterministic graph quality and model quality remain separate measurements.

OpenAI, Anthropic, Gemini, and ComplyScan Cloud remain reserved types only. Any future remote provider must add an explicit disclosure and consent boundary, preserve secret redaction, minimise source disclosure, and keep model observations separate from legal conclusions.
