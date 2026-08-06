# Architecture

ComplyScan v0.1.2 has an offline-by-default pipeline:

```text
target directory
  ├─ validated, version-controlled system profiles
  │    → provisional applicability and high-risk screening
  │    → activity- and deployment-sensitive objective screening
  └─ bounded repository discovery and file classification
       → language-neutral relationship graph and technical-objective matching
       → typed AI provider and framework inventory
       → independently registered deterministic rules and findings
  → deterministic requirement/evidence reconciliation
       → mapped candidate evidence, non-detections, mismatches, and unresolved ownership
  → optional, separate advisory Ollama reviews of findings and technical candidates
  → terminal output plus atomic Markdown and schema-version 2 JSON reports, or SARIF
```

Installation and onboarding are a separate pre-scan path: the POSIX installer selects a release archive for the host platform, verifies it against `checksums.txt`, atomically places the binary in the selected user directory, and invokes `complyscan setup` only when a terminal is present. The setup command reuses `internal/profile` for factual and attributable applicability collection, preserves existing repository configuration, and keeps external provisioning behind individual confirmations. It may invoke Homebrew or Ollama's official Linux installer, start a Homebrew Ollama service, and run `ollama pull`; none of these operations occur during a normal scan.

`internal/discovery` owns filesystem traversal and file classification. It returns repository-relative slash-separated paths so findings are stable across macOS, Linux, and Windows. It skips nested repositories by default and supports Git-tracked-only scans, progress callbacks, and explicit scan budgets.

`internal/profile` owns declared system context, human review attribution, and conservative applicability screening. A repository may contain multiple systems. Controlled values make unknown context and spelling errors visible, while factual free-text labels are bounded. Explicit AI activities distinguish inference, training, fine-tuning, evaluation, automated decisions, agent tool use, and synthetic-content generation. Automated EU AI Act scope and possible high-risk signals are reported separately from human decisions and never produce a compliance verdict. The profile is available to CI through `.complyscan.yml`; interactive questions occur only during setup. Before every prompt, a centrally tested guidance catalog explains the requested fact, defines controlled categories, and gives practical developer examples. It directs users without a documented accountable legal conclusion to retain `needs-review`. Profiles created before the activity field was introduced remain valid but produce missing-context results until updated.

`internal/framework` owns the embedded technical-pack schema, strict parsing, content digest, code-evidence matcher, and objective-specific reports. The CLI pack contains only objectives that inspect source, configuration, tests, CI, containers, dependency declarations, or infrastructure. It records Article references for traceability but does not activate legal controls, inspect uploaded documents, or roll objectives into a compliance status. The first-version matcher requires an eligible file kind, a configured path signal, and every grouped content signal. Matches are `candidate-evidence`; an unmatched check is `not-detected`, never proof that an implementation is missing. Definition files can carry the narrow `complyscan:ignore-technical-evidence` marker to avoid self-identification. Pack content changes require a new version and digest.

`internal/codegraph` owns a language-neutral symbol, import, edge, and reachability model. Go is parsed with the standard library. The dependency-free Python indexer conservatively recognizes indentation-bounded functions, classes, methods, imports, calls, `__main__` entry points, tests, FastAPI and Flask decorators, Django route registrations, dependency-based authorization, configuration access, persistence, and audit/logging calls. The dependency-free JavaScript and TypeScript indexer recognizes functions, arrows, classes, methods, interfaces/types, ESM and CommonJS imports, calls, top-level entry calls, test callbacks, Express/Fastify registrations, Next.js route exports, NestJS decorators and guards, environment access, persistence, and logging. Comments, strings, and template literals are masked before structural extraction. Production entry points and test entry points are traversed separately so test-only or unreached candidates remain visible. Each technical match receives a maximum two-hop context package with report-safe symbol metadata, imports, relationships, and unresolved questions. Source excerpts remain in unexported graph state and cannot enter graph JSON. Unsupported or unparseable source remains visible as incomplete coverage and causes unmatched source objectives to be `not-evaluated`.

`internal/inventory` extracts typed signals from dependency declarations, source imports, recognised endpoints, and actual environment-variable access. It aggregates those signals into a versioned component report with scope, evidence type, package version, confidence, and location fields. Detection-signature files can carry the narrow `complyscan:ignore-ai-signals` marker so synthetic definitions do not self-identify as runtime components.

`internal/reconciliation` owns the versioned applicability-to-objective mapping. It first screens each technical objective from the declared scope, possible high-risk classification, human applicability decision, AI activities, and deployment model. It then combines that requirement status with the independently produced objective evidence and component inventory. Results explicitly distinguish likely requirements with candidate evidence, likely requirements without detected evidence, unclear applicability, configuration/evidence mismatches, incomplete evaluation, and evidence without system ownership. Repository-wide evidence is provisionally associated only when exactly one system is declared. With zero systems it remains unassigned; with multiple systems it remains unassigned until a future path-to-system ownership mapping exists. This avoids inventing system attribution.

`internal/rules` owns the severity and finding models plus the deterministic rule interface. Rules receive a read-only repository snapshot. During ordinary scans every rule sees the full snapshot. During `--changed-since` scans, file-local rules receive only committed, staged, unstaged, and untracked changed files; rules implementing `RepositoryWideRule` retain the full snapshot. The documentation and risk-evidence checks use that interface so a small pull request cannot bypass repository-level governance.

`internal/governance` renders AI-system and risk-assessment scaffolds from structured inventory evidence. Generators preserve discovery warnings, label documents as drafts, require human completion, and protect existing documents unless overwrite is explicit.

`internal/baseline` stores deterministic finding identities without source evidence. Configured suppressions require a review reason; both mechanisms are applied before streaming, reporting, and exit-code evaluation.

`internal/report` constructs a schema-version 2 evidence bundle with scan identity, UTC timestamp, tool build, explicit scope, applicability input, AI inventory, technical evidence, and deterministic reconciliation. Terminal output streams deterministic findings live and then prints the mapped result. Every successful scan atomically replaces `.complyscan/reports/latest.md` and `latest.json`; generated reports are excluded from discovery, target-relative output cannot escape the repository, and symlink artifact destinations are refused. Markdown is the human report and JSON is the future dashboard contract. SARIF 2.1.0 remains a separate source-location integration. Finding observations are attached by finding fingerprint; technical observations are attached by objective ID and evidence fingerprint. Both remain separate from deterministic results and summaries.

`internal/technicalreview` orchestrates one-candidate semantic requests, live progress, and source-context-free reuse. Its OS user-cache key binds the provider, model tag, prompt version, technical-pack identity/digest, objective, evidence fingerprint, and a SHA-256 digest of the complete bounded candidate input. Cached observations are validated against their binding and written atomically with user-only file permissions. Submitted source-context records are not stored; bounded redacted model rationales may still describe repository details.

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
