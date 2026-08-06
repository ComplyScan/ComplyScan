package framework

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"gopkg.in/yaml.v3"
)

const EUAIActTechnicalEvidencePackID = "eu-ai-act-technical-evidence"

//go:embed packs/*.yml
var builtins embed.FS

var identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
var semanticVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

var builtinPaths = map[string]string{
	EUAIActTechnicalEvidencePackID: "packs/eu-ai-act-technical-evidence-v0.1.2.yml",
}

var supportedFileKinds = map[string]struct{}{
	string(discovery.KindSource): {}, string(discovery.KindManifest): {}, string(discovery.KindDockerfile): {},
	string(discovery.KindGitHubAction): {}, string(discovery.KindCI): {}, string(discovery.KindTerraform): {},
	string(discovery.KindEnvTemplate): {}, string(discovery.KindConfig): {},
}

var supportedAIActivities = map[string]struct{}{
	"inference": {}, "training": {}, "fine-tuning": {}, "evaluation": {},
	"automated-decision": {}, "agent-tool-use": {}, "synthetic-content": {},
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
	for _, id := range []string{EUAIActTechnicalEvidencePackID} {
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
	digest := sha256.Sum256(data)
	pack.Digest = hex.EncodeToString(digest[:])
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
	if strings.TrimSpace(pack.Coverage.Framework) == "" || pack.Coverage.EvidenceType != "code" || len(pack.Coverage.Provisions) == 0 || len(pack.Coverage.Limitations) == 0 {
		return errors.New("coverage must declare framework, code evidence type, provisions, and limitations")
	}
	if len(pack.Objectives) == 0 {
		return errors.New("objectives must not be empty")
	}
	seenObjectives := make(map[string]struct{}, len(pack.Objectives))
	for index, objective := range pack.Objectives {
		if !identifierPattern.MatchString(objective.ID) {
			return fmt.Errorf("objectives[%d].id must be a lowercase identifier", index)
		}
		if _, exists := seenObjectives[objective.ID]; exists {
			return fmt.Errorf("objectives[%d].id %q is duplicated", index, objective.ID)
		}
		seenObjectives[objective.ID] = struct{}{}
		if strings.TrimSpace(objective.Title) == "" || strings.TrimSpace(objective.SourceReference) == "" || strings.TrimSpace(objective.Description) == "" {
			return fmt.Errorf("objectives[%d] must declare title, source-reference, and description", index)
		}
		if err := objective.Applicability.Validate(); err != nil {
			return fmt.Errorf("objectives[%d].applicability: %w", index, err)
		}
		if len(objective.FileKinds) == 0 || strings.TrimSpace(objective.Verification) == "" {
			return fmt.Errorf("objectives[%d] must declare file-kinds and verification", index)
		}
		for kindIndex, kind := range objective.FileKinds {
			if _, supported := supportedFileKinds[kind]; !supported {
				return fmt.Errorf("objectives[%d].file-kinds[%d] %q is not a code evidence kind", index, kindIndex, kind)
			}
		}
		if objective.Verification != "technical-and-human" && objective.Verification != "technical-semantic-and-human" {
			return fmt.Errorf("objectives[%d].verification %q is not supported", index, objective.Verification)
		}
		if len(objective.KeywordGroups) == 0 && len(objective.PathKeywords) == 0 {
			return fmt.Errorf("objectives[%d] must declare keyword groups or path keywords", index)
		}
		for _, keyword := range objective.PathKeywords {
			if strings.TrimSpace(keyword) == "" {
				return fmt.Errorf("objectives[%d] contains an empty path keyword", index)
			}
		}
		for groupIndex, group := range objective.KeywordGroups {
			if len(group) == 0 {
				return fmt.Errorf("objectives[%d].keyword-groups[%d] must not be empty", index, groupIndex)
			}
			for _, keyword := range group {
				if strings.TrimSpace(keyword) == "" {
					return fmt.Errorf("objectives[%d] contains an empty keyword", index)
				}
			}
		}
	}
	return nil
}

func (applicability ObjectiveApplicability) Validate() error {
	if applicability.LegalScope != ApplicabilityHighRiskSystem && applicability.LegalScope != ApplicabilityTransparencyObligation {
		return fmt.Errorf("legal-scope %q is not supported", applicability.LegalScope)
	}
	seenActivities := make(map[string]struct{}, len(applicability.ActivitiesAnyOf))
	for index, activity := range applicability.ActivitiesAnyOf {
		if _, supported := supportedAIActivities[activity]; !supported {
			return fmt.Errorf("activities-any-of[%d] %q is not supported", index, activity)
		}
		if _, duplicate := seenActivities[activity]; duplicate {
			return fmt.Errorf("activities-any-of[%d] %q is duplicated", index, activity)
		}
		seenActivities[activity] = struct{}{}
	}
	if applicability.LegalScope == ApplicabilityTransparencyObligation && len(applicability.ActivitiesAnyOf) == 0 {
		return errors.New("activities-any-of is required for a transparency obligation")
	}
	if applicability.ExternalUseRequired && applicability.LegalScope != ApplicabilityTransparencyObligation {
		return errors.New("external-use-required is supported only for transparency obligations")
	}
	return nil
}
