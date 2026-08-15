package cli

import (
	"fmt"
	"strings"
)

type questionGuidance struct {
	choiceDescriptions map[string]string
}

var setupQuestionHelp = map[string][]string{
	"resume-setup": {
		"A private setup draft from this repository is available in your operating system's user-cache directory. It contains configuration answers, but no API-key value or repository source code.",
		"Resume to continue from the last completed major stage, or start over to delete the draft and collect fresh answers.",
	},
	"applicability-context": {
		"ComplyScan now asks only the remaining facts used by the selected EU technical control pack. Later questions appear only when an earlier answer makes them relevant.",
		"You may answer unknown, but the report will then label requirement mapping as provisional. Completing these facts does not itself decide that the EU AI Act applies.",
	},
	"frameworks": {
		"Choose which sources ComplyScan should map the repository's technical evidence against. The EU pack covers code-visible objectives linked to potential legal obligations; NIST AI RMF is voluntary guidance.",
		"This choice controls the technical scan only. It does not mean a law applies, decide legal risk, or certify that any outcome is satisfied. Tick every source you want to scan against; shared evidence is mapped to each selected source separately.",
	},
	"system-id": {
		"A short, stable machine-readable identifier used to connect scans and future dashboard records to this system.",
		"Usually accept the repository-based default. Change it only if your organisation already has a stable system ID.",
	},
	"system-name": {
		"The human-readable product or system name that should appear in reports.",
		"Use the product name, not a customer name, person, secret, or temporary branch name.",
	},
	"intended-purpose": {
		"Describe what the system is designed to do, who uses it, and what outcome it produces.",
		"Example: 'Summarise support tickets for agents; agents verify every draft before sending.'",
		"Describe the real product behavior—not a broad goal such as 'use AI' or 'be compliant'. Enter unknown if it has not been agreed.",
	},
	"lifecycle-stage": {
		"Choose the furthest stage this system has reached:",
		"development — being designed or implemented; not used with real users in normal operation",
		"testing — being validated, piloted, or used in a controlled pre-production environment",
		"production — available or used in normal real-world operation",
		"retired — no longer used, although records or obligations may remain",
		"unknown — the current stage has not been established",
	},
	"organization-roles": {
		"Choose every role your organisation performs. These describe who builds, brands, uses, or supplies the AI system—not the developer's job title.",
		"provider — your organisation develops it, or has it developed, and offers or first uses it under its own name, even for free",
		"deployer — your organisation uses an AI system under its authority in professional operations",
		"importer — an EU-established organisation first places an AI system bearing a non-EU provider's name on the EU market",
		"distributor — an organisation in the supply chain makes another provider's AI system available on the EU market",
		"product-manufacturer — your organisation puts the AI system on the market together with a product under its own name or trademark",
		"unknown — ownership and supply-chain roles have not been confirmed; select unknown alone",
	},
	"operating-regions": {
		"Select where the system is offered, professionally used, or intended to have effects—not merely where the repository or developer is located.",
		"eu — one or more European Union countries",
		"eea — the EU plus Iceland, Liechtenstein, and Norway",
		"uk — the United Kingdom",
		"us — the United States",
		"global — intentionally offered or operated across multiple world regions, including possible EU/EEA use",
		"other — established regions not represented above",
		"unknown — markets and use locations have not been established; select unknown alone",
	},
	"use-case-domains": {
		"Select what the AI actually does. These are screening categories; choosing one does not itself make the system high-risk.",
		"biometrics — identifies people, categorises them from biometric traits, or recognises emotions",
		"critical-infrastructure — controls or supports the safety of essential digital, road, water, gas, heating, or electricity infrastructure",
		"education — admissions, assessment, learning-level decisions, exam monitoring, or vocational training",
		"employment — recruiting, candidate ranking, worker management, task allocation, promotion, termination, or performance monitoring",
		"essential-services — access to benefits, healthcare, credit, insurance, emergency response, housing, or similar essential services",
		"law-enforcement — supports police or other law-enforcement assessments, evidence, profiling, or risk decisions",
		"migration-border-control — visas, asylum, migration, identity, security, or border assessments",
		"justice-democratic-processes — supports courts, legal interpretation, dispute resolution, elections, or voting influence",
		"healthcare — diagnosis, treatment, triage, clinical support, patient administration, or health-related products",
		"software-development — coding, testing, repository analysis, developer assistance, or software operations",
		"general-purpose — a model or capability intended for broad reuse across many unrelated tasks",
		"other — the purpose is established but none of the categories fit; unknown — the purpose/domain has not been established",
	},
	"users": {
		"List the people or professional roles that directly operate or interact with the system.",
		"Examples: recruiters, support agents, doctors, developers, customers. Use categories, not names or personal records.",
	},
	"affected-groups": {
		"List people whose opportunities, access, safety, rights, work, or experience may be influenced—even if they never see the interface.",
		"Examples: job applicants, patients, students, borrowers, employees, content viewers. This can differ from the direct users.",
	},
	"decision-impact": {
		"Choose the strongest effect an AI output can have in the intended workflow. These are ComplyScan screening labels, not statutory legal categories.",
		"advisory — AI suggests or drafts; a person independently reviews before any action",
		"low — AI affects a limited, readily reversible workflow without materially affecting a person's access, safety, or opportunities",
		"significant — AI materially influences eligibility, ranking, access, employment, education, credit, healthcare, safety, or a similarly important outcome",
		"autonomous — AI can take or trigger a consequential action without prior meaningful human approval",
		"unknown — the real downstream use of outputs has not been established",
	},
	"human-oversight": {
		"Describe the oversight that is actually implemented or required in the workflow, not what documentation says should happen.",
		"required — the relevant output or action is blocked until a person reviews it",
		"available — a person can monitor, override, or stop it, but review is not required for every output",
		"limited — intervention exists only for some cases, users, stages, or after the action",
		"none — no person can effectively review, override, or stop the relevant AI behavior",
		"unknown — the operational workflow has not been confirmed",
	},
	"ai-activities": {
		"Select every AI-related activity performed by this system. This helps ComplyScan avoid asking for controls unrelated to the codebase.",
		"inference — sends inputs to a model and receives predictions, recommendations, classifications, embeddings, or generated outputs",
		"training — creates model parameters from training data",
		"fine-tuning — adapts an existing model using your data or examples",
		"evaluation — benchmarks, scores, red-teams, or validates model behavior or quality",
		"automated-decision — uses AI output to make or materially trigger a decision about a person, object, or process",
		"agent-tool-use — allows a model to choose or invoke tools, APIs, commands, files, or external actions",
		"synthetic-content — generates or materially alters text, images, audio, or video that may be delivered as content",
		"unknown — the model-related data flow has not been traced; select unknown alone",
	},
	"personal-data": {
		"Answer yes if the system reads, creates, predicts, stores, logs, or receives information relating to an identified or identifiable person.",
		"Examples include names, email/IP/device identifiers, account IDs, voice or images, location, employee/customer records, and linked pseudonymous IDs.",
		"Answer based on real inputs, outputs, logs, and training/evaluation data—not only the main database schema.",
		"yes — personal data is processed or may enter the system",
		"no — personal data is not processed anywhere in the relevant workflow",
		"unknown — the system's data flows have not been fully established",
	},
	"special-category-data": {
		"Answer yes if personal data reveals racial or ethnic origin, political opinions, religion or beliefs, trade-union membership, genetics, biometric identity, health, sex life, or sexual orientation.",
		"Also choose yes for similarly sensitive categories protected by the laws relevant to your users. Choose unknown if datasets or prompts have not been reviewed.",
		"yes — special-category or similarly sensitive personal data is processed or may enter the system",
		"no — this type of sensitive personal data is not processed in the relevant workflow",
		"unknown — datasets, prompts, outputs, or logs have not been sufficiently reviewed",
	},
	"children-data": {
		"Answer yes if any input, output, account, log, training/evaluation record, or affected-person record can relate to children.",
		"Use the relevant age threshold for the countries and service; choose unknown if age coverage or datasets have not been established.",
		"yes — data about children is processed or may enter the system",
		"no — data about children is not processed in the relevant workflow",
		"unknown — age coverage or the relevant datasets have not been established",
	},
	"deployment-models": {
		"Choose every way the system reaches users or other software:",
		"internal — used only inside your organisation",
		"private-customer — a dedicated customer deployment not open to the public",
		"public — a publicly accessible website, app, or service",
		"open-source — source code or model weights released under an open-source licence",
		"embedded — included as a feature or safety component of another product",
		"api — called programmatically over a service interface",
		"local-cli — runs as a command-line program on the user's machine",
		"unknown — deployment has not been established; select unknown alone",
		"Example: customer-facing SaaS commonly uses public and api; a customer's private instance uses private-customer and possibly api.",
	},
	"profile-reviewer": {
		"Enter the person or accountable role that checked these factual answers against the real system.",
		"Leave unknown if nobody has verified them yet. This keeps the profile visibly in draft instead of implying approval.",
	},
	"applicability-decision": {
		"This is a human legal/applicability record, not a quiz. Most developers should keep needs-review unless an accountable legal or compliance reviewer has documented a conclusion.",
		"needs-review — no reviewed conclusion exists yet",
		"applicable — an accountable reviewer concluded that the EU AI Act applies",
		"not-applicable — an accountable reviewer documented why it does not apply",
		"uncertain — a reviewer assessed it but material uncertainty remains",
		"ComplyScan's provisional screening above is evidence for review, not authority to choose applicable or not-applicable.",
	},
	"decision-rationale": {
		"Summarise the reviewed facts and reasoning supporting the applicability decision.",
		"Do not paste privileged advice, source code, personal data, or secrets; link to an access-controlled assessment if needed.",
	},
	"applicability-reviewer": {
		"Enter the accountable person or role that made or approved the applicability decision.",
		"This should be someone authorised to own the conclusion, not automatically the developer running setup.",
	},
	"replace-profile": {
		"A profile with this system ID already exists. Replacing it updates that system's declared facts with the answers just collected.",
		"Choose no if the existing record belongs to a different system; rerun setup with a different system ID instead.",
	},
	"path-ownership": {
		"When a repository contains more than one declared AI system, ComplyScan needs repository path rules before it can attach code evidence to the correct system.",
		"Without these rules, detected AI code remains visible but unassigned. This avoids guessing that one system implemented another system's controls.",
	},
	"ownership-paths": {
		"Enter one or more repository-relative patterns for code owned by the same system or systems. Patterns use gitignore-style matching.",
		"Examples: services/ranking/**, apps/support/**, or shared/models/**. Do not use absolute paths; unmatched code remains explicitly unassigned.",
	},
	"ownership-systems": {
		"Choose the declared system IDs that own the matching paths. Choose one ID for dedicated code or multiple IDs only when the code is intentionally shared.",
		"Separate overlapping rules with different owners are treated as a conflict and will not be assigned automatically.",
	},
	"replace-ownership": {
		"Replacing ownership removes the current path mappings and saves the new set after validation.",
		"Choose no if you only wanted to inspect the rules; use `complyscan ownership show` to review them without changing configuration.",
	},
	"review-provider": {
		"Every `complyscan scan` is local and model-free. You can optionally configure OpenAI, Anthropic, Gemini, or experimental local Ollama for editable setup suggestions and later explicit `complyscan review` runs.",
		"This choice is made before any model can receive repository context. Cloud assistance sends selected context only during setup after confirmation or an explicit review command; model results remain advisory.",
	},
	"ollama-model": {
		"Ollama is retained as an advanced experimental path for users who cannot send repository context to a cloud provider. No local model is currently approved as ComplyScan's standard reviewer.",
		"Setup lists installed models and candidates, but compatibility does not establish review quality. Small local models may miss connected code, overstate evidence, or draft incorrect questionnaire answers.",
	},
	"install-ollama": {
		"Ollama is a separate local runtime needed only for optional model review. Installation downloads third-party software and may change system packages or start a service.",
		"Choose no to keep deterministic scanning; you can install Ollama later and rerun setup.",
	},
	"download-model": {
		"The selected model weights are separate from the small ComplyScan binary and may require several gigabytes of disk space and substantial download time.",
		"Choose no to save the configuration without downloading; the exact manual command will be printed.",
	},
	"remote-disclosure": {
		"Explicit cloud review sends selected source excerpts, configuration, manifests, infrastructure, and CI evidence after recognised-secret redaction. Selection uses local inventory signals, technical-objective matches, production entry points, and their bounded code-graph neighborhood. Ignored, generated, dependency, binary, oversized, and excluded files remain outside this context.",
		"Redaction is defence in depth, not a guarantee that arbitrary proprietary or personal data has been removed. Use model-free analysis when external processing is not permitted. The explicit deep modes transfer substantially more repository content.",
		"The provider may charge for usage and processes data under your account settings and its terms. Confirm only if your organisation permits this external processing.",
	},
	"remote-provider-name": {
		"Enter a short name that will identify this custom API in setup, diagnostics, and reports.",
		"This is a display label only, such as 'Company model gateway'. Do not enter an API key, account identifier, personal name, or secret.",
	},
	"remote-base-url": {
		"Enter the HTTPS base URL of an API that implements OpenAI-compatible Chat Completions and preferably the Models endpoint.",
		"Example: https://models.example.com/v1. ComplyScan rejects HTTP, embedded credentials, query parameters, and fragments, then appends /models or /chat/completions.",
	},
	"remote-model": {
		"Choose from ComplyScan's small provider-specific frontier-model shortlist. When the API key is available, setup verifies which shortlisted IDs are available to that account instead of presenting an unrestricted catalogue.",
		"Shortlisting is not the same as a completed quality benchmark. Setup labels whether the exact model has passed maintained setup-drafting and technical-review evaluations; model output remains advisory even after validation.",
	},
	"api-key-env": {
		"Enter only the name of the environment variable that contains the API key, such as OPENAI_API_KEY. Do not paste the credential itself.",
		"The variable name is safe to save in .complyscan.yml; the secret value stays in the shell or CI secret store and is never written to reports.",
	},
	"first-scan": {
		"The first scan reads eligible repository files locally, never invokes a model, prints findings, and writes local Markdown and JSON reports under .complyscan/reports.",
		"Choose no if you want to review .complyscan.yml or commit the setup configuration before scanning.",
	},
	"scan-mode": {
		"`complyscan scan` creates the local AI inventory, deterministic findings, and technical mapping without contacting a model. Configuring a provider does not activate it during a scan.",
		"Choose whether to run the local scan now or save first. Use `complyscan review` later when you explicitly want AI-assisted code reasoning.",
	},
}

func (prompt promptSession) clearQuestionGuidance() {
	if prompt.guidance == nil {
		return
	}
	prompt.guidance.choiceDescriptions = nil
}

func explainSetupQuestion(prompt promptSession, key string) error {
	lines, exists := setupQuestionHelp[key]
	if !exists {
		return fmt.Errorf("missing setup guidance for %q", key)
	}
	if _, err := fmt.Fprintln(prompt.output); err != nil {
		return err
	}
	if prompt.guidance != nil {
		prompt.guidance.choiceDescriptions = setupChoiceDescriptions(lines[1:])
	}
	visible := append([]string(nil), lines[0])
	for _, line := range lines[1:] {
		if _, _, isChoiceDescription := strings.Cut(line, " — "); !isChoiceDescription {
			visible = append(visible, line)
		}
	}
	for _, line := range visible {
		if err := writePromptParagraph(prompt.output, "  ", line); err != nil {
			return err
		}
	}
	return nil
}

func setupChoiceDescriptions(lines []string) map[string]string {
	descriptions := make(map[string]string)
	for _, line := range lines {
		value, description, found := strings.Cut(line, " — ")
		value = strings.ToLower(strings.TrimSpace(value))
		description = strings.TrimSpace(description)
		if found && value != "" && description != "" {
			descriptions[value] = description
		}
	}
	return descriptions
}
