package framework

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"gopkg.in/yaml.v3"
)

const EUAIActHighRiskProviderPackID = "eu-ai-act-high-risk-provider"

//go:embed packs/*.yml
var builtins embed.FS

var identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
var semanticVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

var builtinPaths = map[string]string{
	EUAIActHighRiskProviderPackID: "packs/eu-ai-act-high-risk-provider-v0.1.0.yml",
}

var supportedFileKinds = map[string]struct{}{
	string(discovery.KindSource): {}, string(discovery.KindManifest): {}, string(discovery.KindDockerfile): {},
	string(discovery.KindGitHubAction): {}, string(discovery.KindCI): {}, string(discovery.KindTerraform): {},
	string(discovery.KindEnvTemplate): {}, string(discovery.KindReadme): {}, string(discovery.KindDocumentation): {},
	string(discovery.KindModelCard): {}, string(discovery.KindPrivacy): {}, string(discovery.KindRisk): {},
	string(discovery.KindAIGovernance): {}, string(discovery.KindConfig): {}, string(discovery.KindOtherText): {},
}

func LoadBuiltin(id string) (Pack, error) {
	path, ok := builtinPaths[id]
	if !ok {
		return Pack{}, fmt.Errorf("unknown framework pack %q", id)
	}
	data, err := builtins.ReadFile(path)
	if err != nil {
		return Pack{}, fmt.Errorf("read built-in framework pack %q: %w", id, err)
	}
	return Parse(data)
}

func BuiltinPacks() ([]Pack, error) {
	packs := make([]Pack, 0, len(builtinPaths))
	for _, id := range []string{EUAIActHighRiskProviderPackID} {
		pack, err := LoadBuiltin(id)
		if err != nil {
			return nil, err
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

func Parse(data []byte) (Pack, error) {
	var pack Pack
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&pack); err != nil {
		return Pack{}, fmt.Errorf("parse framework pack: %w", err)
	}
	if err := pack.Validate(); err != nil {
		return Pack{}, fmt.Errorf("validate framework pack: %w", err)
	}
	return pack, nil
}

func (pack Pack) Validate() error {
	if pack.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema-version %d", pack.SchemaVersion)
	}
	if !identifierPattern.MatchString(pack.ID) {
		return errors.New("id must be a lowercase identifier")
	}
	if strings.TrimSpace(pack.Name) == "" {
		return errors.New("name must not be empty")
	}
	if !semanticVersionPattern.MatchString(pack.Version) {
		return errors.New("version must use MAJOR.MINOR.PATCH")
	}
	if _, err := time.Parse(time.DateOnly, pack.Released); err != nil {
		return errors.New("released must use YYYY-MM-DD")
	}
	if strings.TrimSpace(pack.Source.Title) == "" || strings.TrimSpace(pack.Source.Reference) == "" || strings.TrimSpace(pack.Source.Edition) == "" {
		return errors.New("source title, reference, and edition must not be empty")
	}
	parsedURL, err := url.Parse(pack.Source.URL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return errors.New("source URL must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(pack.Coverage.Framework) == "" || len(pack.Coverage.Roles) == 0 || strings.TrimSpace(pack.Coverage.RiskClassification) == "" || len(pack.Coverage.Provisions) == 0 || len(pack.Coverage.Limitations) == 0 {
		return errors.New("coverage must declare framework, roles, risk classification, provisions, and limitations")
	}
	if len(pack.Controls) == 0 {
		return errors.New("controls must not be empty")
	}
	seenControls := make(map[string]struct{}, len(pack.Controls))
	for index, control := range pack.Controls {
		if !identifierPattern.MatchString(control.ID) {
			return fmt.Errorf("controls[%d].id must be a lowercase identifier", index)
		}
		if _, exists := seenControls[control.ID]; exists {
			return fmt.Errorf("controls[%d].id %q is duplicated", index, control.ID)
		}
		seenControls[control.ID] = struct{}{}
		if strings.TrimSpace(control.Title) == "" || strings.TrimSpace(control.SourceReference) == "" || strings.TrimSpace(control.Objective) == "" {
			return fmt.Errorf("controls[%d] must declare title, source-reference, and objective", index)
		}
		if len(control.EvidenceRequirements) == 0 {
			return fmt.Errorf("controls[%d].evidence must not be empty", index)
		}
		seenEvidence := make(map[string]struct{}, len(control.EvidenceRequirements))
		for evidenceIndex, evidence := range control.EvidenceRequirements {
			if !identifierPattern.MatchString(evidence.ID) {
				return fmt.Errorf("controls[%d].evidence[%d].id must be a lowercase identifier", index, evidenceIndex)
			}
			if _, exists := seenEvidence[evidence.ID]; exists {
				return fmt.Errorf("controls[%d].evidence[%d].id %q is duplicated", index, evidenceIndex, evidence.ID)
			}
			seenEvidence[evidence.ID] = struct{}{}
			if strings.TrimSpace(evidence.Description) == "" || len(evidence.FileKinds) == 0 || strings.TrimSpace(evidence.Verification) == "" {
				return fmt.Errorf("controls[%d].evidence[%d] must declare description, file-kinds, and verification", index, evidenceIndex)
			}
			for kindIndex, kind := range evidence.FileKinds {
				if _, supported := supportedFileKinds[kind]; !supported {
					return fmt.Errorf("controls[%d].evidence[%d].file-kinds[%d] %q is not supported", index, evidenceIndex, kindIndex, kind)
				}
			}
			if evidence.Verification != "semantic-and-human" && evidence.Verification != "technical-semantic-and-human" {
				return fmt.Errorf("controls[%d].evidence[%d].verification %q is not supported", index, evidenceIndex, evidence.Verification)
			}
			if len(evidence.KeywordGroups) == 0 && len(evidence.PathKeywords) == 0 {
				return fmt.Errorf("controls[%d].evidence[%d] must declare keyword groups or path keywords", index, evidenceIndex)
			}
			for _, keyword := range evidence.PathKeywords {
				if strings.TrimSpace(keyword) == "" {
					return fmt.Errorf("controls[%d].evidence[%d] contains an empty path keyword", index, evidenceIndex)
				}
			}
			for groupIndex, group := range evidence.KeywordGroups {
				if len(group) == 0 {
					return fmt.Errorf("controls[%d].evidence[%d].keyword-groups[%d] must not be empty", index, evidenceIndex, groupIndex)
				}
				for _, keyword := range group {
					if strings.TrimSpace(keyword) == "" {
						return fmt.Errorf("controls[%d].evidence[%d] contains an empty keyword", index, evidenceIndex)
					}
				}
			}
		}
	}
	return nil
}
