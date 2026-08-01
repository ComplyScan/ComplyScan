package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/rules"
	ignore "github.com/sabhiram/go-gitignore"
	"gopkg.in/yaml.v3"
)

const FileName = ".complyscan.yml"

var supportedRules = []string{
	"AI-DISC-001",
	"AI-LOG-001",
	"AI-SEC-001",
	"AI-DOC-001",
	"AI-RISK-001",
}

type Config struct {
	Version      int                   `yaml:"version"`
	Scan         ScanConfig            `yaml:"scan"`
	FailOn       rules.Severity        `yaml:"fail-on"`
	Rules        map[string]RuleConfig `yaml:"rules"`
	AI           AIConfig              `yaml:"ai"`
	Suppressions []Suppression         `yaml:"suppressions,omitempty"`
}

type ScanConfig struct {
	Exclude                   []string `yaml:"exclude"`
	MaxFiles                  int      `yaml:"max-files"`
	MaxTotalBytes             int64    `yaml:"max-total-bytes"`
	IncludeNestedRepositories bool     `yaml:"include-nested-repositories"`
	TrackedOnly               bool     `yaml:"tracked-only"`
}

type RuleConfig struct {
	Enabled bool `yaml:"enabled"`
}

type AIConfig struct {
	Provider string `yaml:"provider"`
}

// Suppression accepts a finding as reviewed. A reason is mandatory so the
// decision remains auditable in version control.
type Suppression struct {
	Rule        string `yaml:"rule,omitempty"`
	Path        string `yaml:"path,omitempty"`
	Fingerprint string `yaml:"fingerprint,omitempty"`
	Reason      string `yaml:"reason"`
}

func Default() Config {
	ruleConfig := make(map[string]RuleConfig, len(supportedRules))
	for _, id := range supportedRules {
		ruleConfig[id] = RuleConfig{Enabled: true}
	}
	return Config{
		Version: 1,
		Scan: ScanConfig{
			Exclude:       []string{"node_modules", "vendor", "dist", "build"},
			MaxFiles:      25_000,
			MaxTotalBytes: 100 << 20,
		},
		FailOn: rules.SeverityHigh,
		Rules:  ruleConfig,
		AI:     AIConfig{Provider: "none"},
	}
}

// Load parses a configuration file on top of the defaults.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := Default()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", path, err)
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported version %d", c.Version)
	}
	if _, err := rules.ParseSeverity(string(c.FailOn)); err != nil {
		return fmt.Errorf("fail-on: %w", err)
	}
	if c.Scan.MaxFiles <= 0 {
		return errors.New("scan.max-files must be greater than zero")
	}
	if c.Scan.MaxTotalBytes <= 0 {
		return errors.New("scan.max-total-bytes must be greater than zero")
	}
	if c.AI.Provider == "" {
		return errors.New("ai.provider must not be empty")
	}
	if c.AI.Provider != "none" {
		return fmt.Errorf("ai.provider %q is not available in v0.1; use none", c.AI.Provider)
	}
	known := make(map[string]struct{}, len(supportedRules))
	for _, id := range supportedRules {
		known[id] = struct{}{}
	}
	for id := range c.Rules {
		if _, ok := known[id]; !ok {
			return fmt.Errorf("unknown rule %q", id)
		}
	}
	for index, suppression := range c.Suppressions {
		if strings.TrimSpace(suppression.Reason) == "" {
			return fmt.Errorf("suppressions[%d].reason must not be empty", index)
		}
		if suppression.Rule == "" && suppression.Fingerprint == "" {
			return fmt.Errorf("suppressions[%d] must set rule or fingerprint", index)
		}
		if suppression.Rule != "" {
			if _, ok := known[suppression.Rule]; !ok {
				return fmt.Errorf("suppressions[%d] has unknown rule %q", index, suppression.Rule)
			}
		}
		if suppression.Fingerprint != "" && !validFingerprint(suppression.Fingerprint) {
			return fmt.Errorf("suppressions[%d].fingerprint must be a 64-character SHA-256 value", index)
		}
	}
	return nil
}

func (c Config) RuleEnabled(id string) bool {
	rule, ok := c.Rules[id]
	return !ok || rule.Enabled
}

// FindingSuppressed reports whether a configured, reasoned suppression
// accepts the finding.
func (c Config) FindingSuppressed(finding rules.Finding) bool {
	for _, suppression := range c.Suppressions {
		if suppression.Rule != "" && suppression.Rule != finding.RuleID {
			continue
		}
		if suppression.Fingerprint != "" && suppression.Fingerprint != finding.Fingerprint {
			continue
		}
		if suppression.Path != "" {
			if finding.Path == "" || !ignore.CompileIgnoreLines(suppression.Path).MatchesPath(filepath.ToSlash(finding.Path)) {
				continue
			}
		}
		return true
	}
	return false
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

// Resolve returns an explicit config or the target-local default when present.
func Resolve(target, explicit string) (Config, string, error) {
	if explicit != "" {
		cfg, err := Load(explicit)
		return cfg, explicit, err
	}
	path := filepath.Join(target, FileName)
	if _, err := os.Stat(path); err == nil {
		cfg, loadErr := Load(path)
		return cfg, path, loadErr
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, path, fmt.Errorf("inspect config %q: %w", path, err)
	}
	return Default(), "", nil
}

// WriteDefault creates the starter configuration.
func WriteDefault(path string, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}

	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
		return fmt.Errorf("create config %q: %w", path, err)
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	if err := encoder.Encode(Default()); err != nil {
		return fmt.Errorf("write config %q: %w", path, err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("close config %q: %w", path, err)
	}
	return nil
}
