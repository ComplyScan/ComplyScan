package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/rules"
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
	Systems      []profile.System      `yaml:"systems,omitempty"`
	Baseline     string                `yaml:"baseline,omitempty"`
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
	Provider string       `yaml:"provider"`
	Ollama   OllamaConfig `yaml:"ollama"`
}

type OllamaConfig struct {
	Endpoint       string `yaml:"endpoint"`
	Model          string `yaml:"model"`
	TimeoutSeconds int    `yaml:"timeout-seconds"`
	MaxFindings    int    `yaml:"max-findings"`
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
			Exclude:       []string{"node_modules", "vendor", "dist", "build", ".complyscan/reports"},
			MaxFiles:      25_000,
			MaxTotalBytes: 100 << 20,
		},
		FailOn: rules.SeverityHigh,
		Rules:  ruleConfig,
		AI: AIConfig{
			Provider: "none",
			Ollama: OllamaConfig{
				Endpoint: "http://127.0.0.1:11434", Model: "qwen3:8b",
				TimeoutSeconds: 300, MaxFindings: 20,
			},
		},
		Baseline: ".complyscan-baseline.json",
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
	if c.AI.Provider != "none" && c.AI.Provider != "ollama" {
		return fmt.Errorf("ai.provider %q is not available; use none or ollama", c.AI.Provider)
	}
	if err := c.AI.Ollama.Validate(); err != nil {
		return fmt.Errorf("ai.ollama: %w", err)
	}
	if err := profile.ValidateSystems(c.Systems); err != nil {
		return err
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

func (c OllamaConfig) Validate() error {
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("model must not be empty")
	}
	if c.TimeoutSeconds <= 0 || c.TimeoutSeconds > 3600 {
		return errors.New("timeout-seconds must be between 1 and 3600")
	}
	if c.MaxFindings <= 0 || c.MaxFindings > 100 {
		return errors.New("max-findings must be between 1 and 100")
	}
	endpoint, err := url.Parse(c.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return fmt.Errorf("endpoint %q is not a valid URL", c.Endpoint)
	}
	if endpoint.Scheme != "http" {
		return errors.New("endpoint scheme must be http for the local loopback API")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("endpoint must not contain credentials, query parameters, or a fragment")
	}
	path := strings.TrimSuffix(endpoint.EscapedPath(), "/")
	if path != "" && path != "/api" {
		return errors.New("endpoint path must be empty or /api")
	}
	hostname := endpoint.Hostname()
	address := net.ParseIP(hostname)
	if !strings.EqualFold(hostname, "localhost") && (address == nil || !address.IsLoopback()) {
		return errors.New("endpoint must use localhost or a loopback IP address")
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
	return Write(path, Default(), force)
}

// Write creates a configuration after validating it.
func Write(path string, cfg Config, force bool) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("encode config %q: %w", path, err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("finish config %q: %w", path, err)
	}
	if force {
		return writeAtomic(path, encoded.Bytes())
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
		return fmt.Errorf("create config %q: %w", path, err)
	}
	if _, err := file.Write(encoded.Bytes()); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write config %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close config %q: %w", path, err)
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect config %q: %w", path, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temporary config for %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set temporary config permissions for %q: %w", path, err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary config for %q: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary config for %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config for %q: %w", path, err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace config %q: %w", path, err)
	}
	return nil
}
