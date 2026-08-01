package rules

func DefaultRules() []Rule {
	return []Rule{
		AIUsageRule{},
		PromptLoggingRule{},
		HardcodedSecretRule{},
		MissingDocumentationRule{},
		MissingRiskClassificationRule{},
	}
}
