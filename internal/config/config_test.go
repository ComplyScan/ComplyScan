package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

func TestLoadMergesDefaultsAndParsesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	content := `version: 1
scan:
  exclude:
    - generated
fail-on: medium
rules:
  AI-LOG-001:
    enabled: false
ai:
  provider: none
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FailOn != rules.SeverityMedium {
		t.Fatalf("fail-on = %q", cfg.FailOn)
	}
	if cfg.RuleEnabled("AI-LOG-001") {
		t.Fatal("AI-LOG-001 should be disabled")
	}
	if !cfg.RuleEnabled("AI-SEC-001") {
		t.Fatal("unspecified default rule should stay enabled")
	}
	if len(cfg.Scan.Exclude) != 1 || cfg.Scan.Exclude[0] != "generated" {
		t.Fatalf("exclude = %#v", cfg.Scan.Exclude)
	}
	if cfg.Scan.MaxFiles != 25_000 || cfg.Scan.MaxTotalBytes != 100<<20 {
		t.Fatalf("scan budgets = %d files, %d bytes", cfg.Scan.MaxFiles, cfg.Scan.MaxTotalBytes)
	}
	if cfg.AI.Ollama.Model != "qwen3:8b" {
		t.Fatalf("default Ollama model = %q", cfg.AI.Ollama.Model)
	}
	if len(cfg.Frameworks) != 1 || cfg.Frameworks[0] != framework.EUAIActTechnicalEvidencePackID {
		t.Fatalf("default frameworks = %#v", cfg.Frameworks)
	}
}

func TestLoadAcceptsMultipleFrameworkPacks(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	content := `version: 1
frameworks:
  - eu-ai-act-technical-evidence
  - nist-ai-rmf-technical-evidence
fail-on: high
ai:
  provider: none
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Frameworks) != 2 {
		t.Fatalf("frameworks = %#v", cfg.Frameworks)
	}
}

func TestValidateRejectsUnknownOrDuplicateFrameworkPack(t *testing.T) {
	for _, ids := range [][]string{{"unknown"}, {framework.EUAIActTechnicalEvidencePackID, framework.EUAIActTechnicalEvidencePackID}} {
		cfg := Default()
		cfg.Frameworks = ids
		if err := cfg.Validate(); err == nil {
			t.Fatalf("frameworks %#v should be rejected", ids)
		}
	}
}

func TestLoadRejectsInvalidSeverity(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("version: 1\nfail-on: urgent\nai:\n  provider: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "invalid severity") {
		t.Fatalf("got error %v", err)
	}
}

func TestLoadRejectsUnknownFieldsRulesAndProviders(t *testing.T) {
	tests := []string{
		"version: 1\nfail-on: high\ntyop: true\nai:\n  provider: none\n",
		"version: 1\nfail-on: high\nrules:\n  AI-TYPO-001:\n    enabled: true\nai:\n  provider: none\n",
		"version: 1\nfail-on: high\nai:\n  provider: unsupported\n",
	}
	for _, content := range tests {
		path := filepath.Join(t.TempDir(), FileName)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("expected invalid config error for:\n%s", content)
		}
	}
}

func TestLoadAcceptsRemoteProviderWithoutCredentialValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	content := `version: 1
fail-on: high
ai:
  provider: openai
  remote:
    model: gpt-5.6-terra
    api-key-env: OPENAI_API_KEY
    timeout-seconds: 120
    max-findings: 10
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Provider != "openai" || cfg.AI.Remote.Model != "gpt-5.6-terra" || cfg.AI.Remote.APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("unexpected remote config: %#v", cfg.AI)
	}
	if strings.Contains(content, "sk-") {
		t.Fatal("test fixture unexpectedly contains a credential value")
	}
}

func TestLoadAcceptsExplicitPathOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	cfg := Default()
	first := profile.NewDraftSystem("ranking", "Ranking")
	second := profile.NewDraftSystem("support", "Support")
	cfg.Systems = []profile.System{first, second}
	cfg.Ownership = []ownership.Rule{
		{Paths: []string{"services/ranking/**"}, Systems: []string{"ranking"}},
		{Paths: []string{"shared/models/**"}, Systems: []string{"ranking", "support"}},
	}
	if err := Write(path, cfg, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ownership) != 2 || len(loaded.Ownership[1].Systems) != 2 {
		t.Fatalf("ownership = %#v", loaded.Ownership)
	}
}

func TestValidateRejectsOwnershipForUndeclaredSystem(t *testing.T) {
	cfg := Default()
	cfg.Systems = []profile.System{profile.NewDraftSystem("ranking", "Ranking")}
	cfg.Ownership = []ownership.Rule{{Paths: []string{"services/**"}, Systems: []string{"missing"}}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "undeclared system") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateRejectsUnsafeRemoteConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*RemoteConfig)
	}{
		{name: "missing model", change: func(cfg *RemoteConfig) { cfg.Model = "" }},
		{name: "missing key environment", change: func(cfg *RemoteConfig) { cfg.APIKeyEnv = "" }},
		{name: "credential instead of environment", change: func(cfg *RemoteConfig) { cfg.APIKeyEnv = "sk-secret-value" }},
		{name: "invalid timeout", change: func(cfg *RemoteConfig) { cfg.TimeoutSeconds = 0 }},
		{name: "too many findings", change: func(cfg *RemoteConfig) { cfg.MaxFindings = 101 }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := Default()
			cfg.AI.Provider = "openai"
			cfg.AI.Remote = RemoteConfig{Model: "gpt-5.6-terra", APIKeyEnv: "OPENAI_API_KEY", TimeoutSeconds: 120, MaxFindings: 20}
			testCase.change(&cfg.AI.Remote)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected invalid remote configuration: %#v", cfg.AI.Remote)
			}
		})
	}
}

func TestLoadAcceptsLocalOllamaProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	content := `version: 1
fail-on: high
ai:
  provider: ollama
  ollama:
    endpoint: http://localhost:11434
    model: qwen2.5-coder:7b
    timeout-seconds: 90
    max-findings: 12
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Provider != "ollama" || cfg.AI.Ollama.Model != "qwen2.5-coder:7b" || cfg.AI.Ollama.MaxFindings != 12 {
		t.Fatalf("unexpected Ollama config: %#v", cfg.AI)
	}
}

func TestLoadParsesReusableVerificationRecipes(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	content := `version: 1
fail-on: high
ai:
  provider: none
verification:
  recipes:
    - id: override-tests
      runtime: docker
      image: golang:1.25
      command: go
      args: [test, ./control/...]
      objectives: [eu-aia-14-override-intervention]
      systems: [ranking]
      timeout-seconds: 120
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Verification == nil || len(cfg.Verification.Recipes) != 1 || cfg.Verification.Recipes[0].Arguments[1] != "./control/..." {
		t.Fatalf("unexpected verification recipes: %#v", cfg.Verification)
	}
}

func TestValidateRejectsUnsafeVerificationRecipes(t *testing.T) {
	base := VerificationRecipe{
		ID: "tests", Runtime: "docker", Image: "golang:1.25", Command: "go",
		Objectives: []string{"objective"},
	}
	tests := []struct {
		name   string
		change func(*VerificationRecipe)
	}{
		{name: "invalid id", change: func(recipe *VerificationRecipe) { recipe.ID = "test command" }},
		{name: "invalid runtime", change: func(recipe *VerificationRecipe) { recipe.Runtime = "shell" }},
		{name: "option image", change: func(recipe *VerificationRecipe) { recipe.Image = "--privileged" }},
		{name: "shell-like command", change: func(recipe *VerificationRecipe) { recipe.Command = "go test" }},
		{name: "missing objective", change: func(recipe *VerificationRecipe) { recipe.Objectives = nil }},
		{name: "duplicate objective", change: func(recipe *VerificationRecipe) { recipe.Objectives = []string{"objective", "objective"} }},
		{name: "excessive timeout", change: func(recipe *VerificationRecipe) { recipe.TimeoutSeconds = 1801 }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			recipe := base
			testCase.change(&recipe)
			cfg := Default()
			cfg.Verification = &VerificationConfig{Recipes: []VerificationRecipe{recipe}}
			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected invalid recipe: %#v", recipe)
			}
		})
	}
}

func TestLoadParsesValidatedSystemProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	content := `version: 1
fail-on: high
ai:
  provider: none
systems:
  - id: candidate-ranking
    name: Candidate ranking
    intended-purpose: Rank job applications for recruiter review.
    lifecycle-stage: development
    organization-roles: [provider]
    operating-regions: [eu]
    use-case-domains: [employment]
    users: [recruiters]
    affected-groups: [job applicants]
    decision-impact: advisory
    human-oversight: required
    ai-activities: [inference, automated-decision]
    data:
      personal-data: yes
      special-category-data: unknown
      children-data: no
    deployment-models: [private-customer]
    profile-review:
      status: draft
    applicability:
      - framework: eu-ai-act
        status: needs-review
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Systems) != 1 || cfg.Systems[0].UseCaseDomains[0] != profile.DomainEmployment || len(cfg.Systems[0].AIActivities) != 2 {
		t.Fatalf("unexpected systems: %#v", cfg.Systems)
	}
}

func TestValidateRejectsUnsafeOllamaConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{name: "remote endpoint", change: func(cfg *Config) { cfg.AI.Ollama.Endpoint = "https://example.com" }},
		{name: "TLS endpoint", change: func(cfg *Config) { cfg.AI.Ollama.Endpoint = "https://localhost:11434" }},
		{name: "credentials", change: func(cfg *Config) { cfg.AI.Ollama.Endpoint = "http://user:pass@localhost:11434" }},
		{name: "missing model", change: func(cfg *Config) { cfg.AI.Ollama.Model = "" }},
		{name: "invalid timeout", change: func(cfg *Config) { cfg.AI.Ollama.TimeoutSeconds = 0 }},
		{name: "too many findings", change: func(cfg *Config) { cfg.AI.Ollama.MaxFindings = 101 }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := Default()
			cfg.AI.Provider = "ollama"
			testCase.change(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected invalid configuration: %#v", cfg.AI.Ollama)
			}
		})
	}
}

func TestLoadRejectsInvalidScanBudgets(t *testing.T) {
	for _, field := range []string{"max-files: 0", "max-total-bytes: 0"} {
		path := filepath.Join(t.TempDir(), FileName)
		content := "version: 1\nscan:\n  " + field + "\nfail-on: high\nai:\n  provider: none\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "must be greater than zero") {
			t.Fatalf("got error %v for %q", err, field)
		}
	}
}

func TestFindingSuppressedMatchesRulePathAndFingerprint(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	finding := rules.Finding{RuleID: "AI-LOG-001", Path: "testdata/nested/app.py", Fingerprint: fingerprint}
	cfg := Default()
	cfg.Suppressions = []Suppression{
		{Rule: "AI-SEC-001", Reason: "different rule"},
		{Rule: "AI-LOG-001", Path: "testdata/**", Fingerprint: fingerprint, Reason: "synthetic fixture"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if !cfg.FindingSuppressed(finding) {
		t.Fatal("expected finding to be suppressed")
	}
	finding.Path = "internal/app.py"
	if cfg.FindingSuppressed(finding) {
		t.Fatal("path outside the pattern was suppressed")
	}
}

func TestValidateRejectsUnreasonedSuppression(t *testing.T) {
	cfg := Default()
	cfg.Suppressions = []Suppression{{Rule: "AI-LOG-001"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("got error %v", err)
	}
}

func TestWriteDefaultDoesNotOverwriteWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := WriteDefault(path, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefault(path, false); err == nil {
		t.Fatal("expected existing-file error")
	}
	if err := WriteDefault(path, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("generated config does not parse: %v", err)
	}
}

func TestForcedWriteAtomicallyPreservesPermissionsAndUpdatesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.FailOn = rules.SeverityMedium
	if err := Write(path, cfg, true); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.FailOn != rules.SeverityMedium {
		t.Fatalf("fail-on = %q", loaded.FailOn)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files=%v error=%v", matches, err)
	}
}
