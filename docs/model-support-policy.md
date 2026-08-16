# Model support policy

ComplyScan separates model availability, protocol compatibility, and measured review quality. These are different claims:

- **Available** means the provider account can access the exact model ID.
- **Compatible** means the model passed ComplyScan's source-free structured-output and binding qualification.
- **Validated for setup drafting** means the exact model and prompt version passed the labelled profile-draft gate.
- **Validated for technical review** means the exact model and prompt version passed every maintained adversarial technical-review fixture.

Compatibility never earns either validation label. A model is promoted to the standard validated set only after both live gates pass and the dated results are reviewed and recorded. Every model result remains advisory after promotion.

## Standard setup

The normal interactive setup recommends BYOK cloud review and exposes only this small quality-oriented candidate set:

| Provider | Exact model ID | Intended role | Current ComplyScan quality status |
| --- | --- | --- | --- |
| OpenAI | `gpt-5.6-sol` | Quality-first reasoning and code review | Both live benchmarks pending |
| OpenAI | `gpt-5.6-terra` | Balanced quality, latency, and cost | Both live benchmarks pending |
| Anthropic | `claude-opus-5` | Quality-first complex reasoning and code review | Both live benchmarks pending |
| Anthropic | `claude-sonnet-5` | Balanced quality, latency, and cost | Both live benchmarks pending |
| Google Gemini | `gemini-3.5-flash` | Quality-first sustained coding and agentic analysis | Both live benchmarks pending |
| Google Gemini | `gemini-3.6-flash` | Balanced agentic capability and efficiency | Both live benchmarks pending |

The setup labels this status directly. It does not describe any pending candidate as validated. Model IDs are intentionally exact rather than unrestricted `latest` aliases. Updating this table requires provider-documentation review, adapter compatibility tests, and both ComplyScan live quality gates.

Candidate selection was checked against the providers' current primary documentation: [OpenAI model guidance](https://developers.openai.com/api/docs/guides/latest-model), [Anthropic model selection](https://platform.claude.com/docs/en/about-claude/models/choosing-a-model), and [Gemini model documentation](https://ai.google.dev/gemini-api/docs/models). Provider capability descriptions are only selection inputs; ComplyScan's own gates determine whether an exact model earns a task-specific validation label.

When an API key is available, setup queries the provider only to determine which shortlisted IDs the account can use. It does not turn the provider's complete model catalogue into normal ComplyScan choices. Existing explicit configuration and non-interactive flags remain backward compatible, but models and providers outside the shortlist are experimental and receive no maintained quality claim.

## Model-free and local paths

`complyscan scan` always runs deterministic repository rules, inventory, graph construction, technical-objective matching, reconciliation, reporting, and configured failure thresholds. Current guided setup records both `ai.review-on-scan: true` repository intent and matching private trust on that machine after a user selects a model boundary; the same command can then add semantic evidence investigation. A clone or ephemeral runner has no transferable trust and remains deterministic unless setup runs locally or provider options supply explicit one-run consent. `complyscan scan --deterministic-only` remains available without a model, API key, or external processing. A missing AI dependency preserves deterministic results, `--require-ai-review` is policy rather than consent, and model verdicts never alter deterministic finding thresholds.

Ollama remains available as an advanced experimental path for organisations that cannot send repository context to a hosted provider. No local model is currently approved as ComplyScan's standard reviewer. Historical `qwen3:8b` and `qwen3.5:9b` results remain attributed to their exact fixtures and prompt versions; they do not justify a general local-review claim.

## Running the live gates

The evaluator accepts the API-key **environment-variable name**, never the credential value. For example:

```bash
export OPENAI_API_KEY="your-key-from-your-secret-store"
./scripts/validate-cloud-model.sh openai gpt-5.6-sol OPENAI_API_KEY
```

The command performs the maintained setup-draft gate and all Go, Python, and TypeScript technical-review fixtures. It makes paid provider calls and therefore runs only when a maintainer invokes it explicitly. Results are written under `.complyscan/validation/cloud-model/`, which is excluded from repository scanning and should be reviewed before a validation flag is changed in `internal/cli/hosted_profiles.go`.

Promotion requires:

1. every maintained gate to pass with the exact model ID and current prompt contracts;
2. no forbidden questionnaire inference or ungrounded path citation;
3. valid structured output and exact trusted binding;
4. correct production-versus-test interpretation across all technical fixtures;
5. acceptable latency and cost recorded alongside the dated result; and
6. rerunning the gates after a model, prompt, schema, retrieval, or material guardrail change.

## Future specialised local model

A specialised downloadable model remains a future option, not a current product claim. The likely route is to build a consented, source-safe labelled corpus, measure failure categories, fine-tune or distil a suitable open-weight code model, and require the same gates used for hosted providers. Model weights should remain an optional separately versioned download rather than increasing the base CLI package until their quality, licence, provenance, update, and security lifecycle are established.
