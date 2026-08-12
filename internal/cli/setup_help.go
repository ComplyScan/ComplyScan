package cli

import (
	"fmt"
)

type questionGuidance struct {
	details []string
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
	"organization-role-basic": {
		"Choose the closest description of your organisation's relationship to this system, not your personal job title.",
		"This question covers provider and deployer roles. Importer, distributor, and product-manufacturer roles can be added later in the detailed system profile.",
	},
	"operating-regions": {
		"Select where the system is offered, professionally used, or intended to have effects—not merely where the repository or developer is located.",
		"eu — one or more European Union countries; eea — EU plus Iceland, Liechtenstein, or Norway",
		"uk / us — the United Kingdom or United States respectively",
		"global — intentionally offered or operated across multiple world regions, including possible EU/EEA use",
		"other — established regions not represented above; unknown — markets and use locations have not been established",
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
		"training — creates model parameters from training data; fine-tuning — adapts an existing model using your data or examples",
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
	},
	"special-category-data": {
		"Answer yes if personal data reveals racial or ethnic origin, political opinions, religion or beliefs, trade-union membership, genetics, biometric identity, health, sex life, or sexual orientation.",
		"Also choose yes for similarly sensitive categories protected by the laws relevant to your users. Choose unknown if datasets or prompts have not been reviewed.",
	},
	"children-data": {
		"Answer yes if any input, output, account, log, training/evaluation record, or affected-person record can relate to children.",
		"Use the relevant age threshold for the countries and service; choose unknown if age coverage or datasets have not been established.",
	},
	"deployment-models": {
		"Choose every way the system reaches users or other software:",
		"internal — used only inside your organisation; private-customer — a dedicated customer deployment not open to the public",
		"public — publicly accessible website, app, or service; open-source — source or model released under an open-source licence",
		"embedded — included as a feature or safety component of another product; api — called programmatically over a service interface",
		"local-cli — runs as a command-line program on the user's machine; unknown — deployment has not been established",
		"Example: customer-facing SaaS commonly uses public and api; a customer's private instance uses private-customer and possibly api.",
	},
	"profile-reviewer": {
		"Enter the person or accountable role that checked these factual answers against the real system.",
		"Leave unknown if nobody has verified them yet. This keeps the profile visibly in draft instead of implying approval.",
	},
	"applicability-decision": {
		"This is a human legal/applicability record, not a quiz. Most developers should keep needs-review unless an accountable legal or compliance reviewer has documented a conclusion.",
		"needs-review — no reviewed conclusion exists yet; applicable — a reviewer concluded the EU AI Act applies",
		"not-applicable — a reviewer documented why it does not apply; uncertain — a reviewer assessed it but material uncertainty remains",
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
		"Choose local AI to keep bounded model context on this machine, cloud AI to use your own provider account, or fast technical analysis to run without a model.",
		"This choice is made before repository analysis so no model receives context before its privacy boundary is clear. Every model result remains an advisory draft that requires confirmation.",
	},
	"ollama-model": {
		"Choose the local Ollama model used for advisory code-context review. Setup lists installed models and recommendations; qwen3.5:9b is the default candidate, while qwen3:8b retains the previous live-validation baseline.",
		"You may enter any exact Ollama tag. A different model may use more memory, run more slowly, or fail ComplyScan's structured-output contract until independently validated.",
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
		"Remote review sends bounded, secret-redacted finding records and selected source-code excerpts to the chosen provider. The complete repository and system profile are not uploaded.",
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
		"Choose the exact remote model used for advisory review. When the API key is available, setup loads the models currently available to that account; otherwise it shows suggested IDs and manual entry.",
		"Models differ in cost, latency, structured-output behavior, and review quality. ComplyScan applies the same bounded schema and guardrails to every provider.",
	},
	"api-key-env": {
		"Enter only the name of the environment variable that contains the API key, such as OPENAI_API_KEY. Do not paste the credential itself.",
		"The variable name is safe to save in .complyscan.yml; the secret value stays in the shell or CI secret store and is never written to reports.",
	},
	"first-scan": {
		"The first scan reads eligible repository files locally, prints findings, and writes local Markdown and JSON reports under .complyscan/reports.",
		"Choose no if you want to review .complyscan.yml or commit the setup configuration before scanning.",
	},
	"scan-mode": {
		"ComplyScan always creates the local AI inventory, deterministic findings, and technical mapping. When an AI provider is configured, the same scan also investigates ambiguous evidence.",
		"Choose whether to run the first scan now or save the configuration first. Later scans use the same `complyscan scan` command locally and in CI.",
	},
}

func (prompt promptSession) hasQuestionGuidance() bool {
	return prompt.guidance != nil && len(prompt.guidance.details) > 0
}

func (prompt promptSession) showQuestionGuidance() error {
	if !prompt.hasQuestionGuidance() {
		return nil
	}
	if err := prompt.sectionTitle("More guidance", true); err != nil {
		return err
	}
	for _, line := range prompt.guidance.details {
		if err := writePromptParagraph(prompt.output, "  ", line); err != nil {
			return err
		}
	}
	if prompt.selectOne == nil {
		_, err := fmt.Fprintln(prompt.output, "  Continue by answering the question below.")
		return err
	}
	return nil
}

func (prompt promptSession) clearQuestionGuidance() {
	if prompt.guidance == nil {
		return
	}
	prompt.guidance.details = nil
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
		prompt.guidance.details = append(prompt.guidance.details[:0], lines[1:]...)
	}
	visible := lines[:1]
	if prompt.alwaysDetailed {
		visible = lines
		if prompt.guidance != nil {
			prompt.guidance.details = nil
		}
	}
	for _, line := range visible {
		if err := writePromptParagraph(prompt.output, "  ", line); err != nil {
			return err
		}
	}
	return nil
}
