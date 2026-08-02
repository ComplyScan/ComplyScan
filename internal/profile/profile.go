package profile

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type LifecycleStage string
type OrganizationRole string
type OperatingRegion string
type UseCaseDomain string
type DecisionImpact string
type HumanOversight string
type TriState string
type DeploymentModel string
type ReviewStatus string
type ApplicabilityStatus string

const (
	LifecycleDevelopment LifecycleStage = "development"
	LifecycleTesting     LifecycleStage = "testing"
	LifecycleProduction  LifecycleStage = "production"
	LifecycleRetired     LifecycleStage = "retired"
	LifecycleUnknown     LifecycleStage = "unknown"

	RoleProvider            OrganizationRole = "provider"
	RoleDeployer            OrganizationRole = "deployer"
	RoleImporter            OrganizationRole = "importer"
	RoleDistributor         OrganizationRole = "distributor"
	RoleProductManufacturer OrganizationRole = "product-manufacturer"
	RoleUnknown             OrganizationRole = "unknown"

	RegionEU      OperatingRegion = "eu"
	RegionEEA     OperatingRegion = "eea"
	RegionUK      OperatingRegion = "uk"
	RegionUS      OperatingRegion = "us"
	RegionGlobal  OperatingRegion = "global"
	RegionOther   OperatingRegion = "other"
	RegionUnknown OperatingRegion = "unknown"

	DomainBiometrics               UseCaseDomain = "biometrics"
	DomainCriticalInfrastructure   UseCaseDomain = "critical-infrastructure"
	DomainEducation                UseCaseDomain = "education"
	DomainEmployment               UseCaseDomain = "employment"
	DomainEssentialServices        UseCaseDomain = "essential-services"
	DomainLawEnforcement           UseCaseDomain = "law-enforcement"
	DomainMigrationBorderControl   UseCaseDomain = "migration-border-control"
	DomainJusticeDemocraticProcess UseCaseDomain = "justice-democratic-processes"
	DomainHealthcare               UseCaseDomain = "healthcare"
	DomainSoftwareDevelopment      UseCaseDomain = "software-development"
	DomainGeneralPurpose           UseCaseDomain = "general-purpose"
	DomainOther                    UseCaseDomain = "other"
	DomainUnknown                  UseCaseDomain = "unknown"

	ImpactAdvisory    DecisionImpact = "advisory"
	ImpactLow         DecisionImpact = "low"
	ImpactSignificant DecisionImpact = "significant"
	ImpactAutonomous  DecisionImpact = "autonomous"
	ImpactUnknown     DecisionImpact = "unknown"

	OversightRequired  HumanOversight = "required"
	OversightAvailable HumanOversight = "available"
	OversightLimited   HumanOversight = "limited"
	OversightNone      HumanOversight = "none"
	OversightUnknown   HumanOversight = "unknown"

	TriYes     TriState = "yes"
	TriNo      TriState = "no"
	TriUnknown TriState = "unknown"

	DeploymentInternal        DeploymentModel = "internal"
	DeploymentPrivateCustomer DeploymentModel = "private-customer"
	DeploymentPublic          DeploymentModel = "public"
	DeploymentOpenSource      DeploymentModel = "open-source"
	DeploymentEmbedded        DeploymentModel = "embedded"
	DeploymentAPI             DeploymentModel = "api"
	DeploymentLocalCLI        DeploymentModel = "local-cli"
	DeploymentUnknown         DeploymentModel = "unknown"

	ReviewDraft     ReviewStatus = "draft"
	ReviewConfirmed ReviewStatus = "confirmed"

	ApplicabilityNeedsReview   ApplicabilityStatus = "needs-review"
	ApplicabilityApplicable    ApplicabilityStatus = "applicable"
	ApplicabilityNotApplicable ApplicabilityStatus = "not-applicable"
	ApplicabilityUncertain     ApplicabilityStatus = "uncertain"
)

const FrameworkEUAIAct = "eu-ai-act"

var systemIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// NewDraftSystem returns an explicit-unknown profile suitable for guided setup.
func NewDraftSystem(id, name string) System {
	return System{
		ID: id, Name: name, IntendedPurpose: "unknown", LifecycleStage: LifecycleUnknown,
		OrganizationRoles: []OrganizationRole{RoleUnknown}, OperatingRegions: []OperatingRegion{RegionUnknown},
		UseCaseDomains: []UseCaseDomain{DomainUnknown}, Users: []string{"unknown"}, AffectedGroups: []string{"unknown"},
		DecisionImpact: ImpactUnknown, HumanOversight: OversightUnknown,
		Data:             DataProfile{PersonalData: TriUnknown, SpecialCategoryData: TriUnknown, ChildrenData: TriUnknown},
		DeploymentModels: []DeploymentModel{DeploymentUnknown}, ProfileReview: ProfileReview{Status: ReviewDraft},
		Applicability: []ApplicabilityDecision{{Framework: FrameworkEUAIAct, Status: ApplicabilityNeedsReview}},
	}
}

// SlugID produces a stable, valid default ID from a directory or system name.
func SlugID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	lastSeparator := false
	for _, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '.' || character == '_'
		if valid {
			result.WriteRune(character)
			lastSeparator = false
		} else if !lastSeparator && result.Len() > 0 {
			result.WriteByte('-')
			lastSeparator = true
		}
		if result.Len() >= 64 {
			break
		}
	}
	return strings.Trim(result.String(), "-._")
}

type System struct {
	ID                string                  `yaml:"id" json:"id"`
	Name              string                  `yaml:"name" json:"name"`
	IntendedPurpose   string                  `yaml:"intended-purpose" json:"intended_purpose"`
	LifecycleStage    LifecycleStage          `yaml:"lifecycle-stage" json:"lifecycle_stage"`
	OrganizationRoles []OrganizationRole      `yaml:"organization-roles" json:"organization_roles"`
	OperatingRegions  []OperatingRegion       `yaml:"operating-regions" json:"operating_regions"`
	UseCaseDomains    []UseCaseDomain         `yaml:"use-case-domains" json:"use_case_domains"`
	Users             []string                `yaml:"users" json:"users"`
	AffectedGroups    []string                `yaml:"affected-groups" json:"affected_groups"`
	DecisionImpact    DecisionImpact          `yaml:"decision-impact" json:"decision_impact"`
	HumanOversight    HumanOversight          `yaml:"human-oversight" json:"human_oversight"`
	Data              DataProfile             `yaml:"data" json:"data"`
	DeploymentModels  []DeploymentModel       `yaml:"deployment-models" json:"deployment_models"`
	ProfileReview     ProfileReview           `yaml:"profile-review" json:"profile_review"`
	Applicability     []ApplicabilityDecision `yaml:"applicability,omitempty" json:"applicability,omitempty"`
}

type DataProfile struct {
	PersonalData        TriState `yaml:"personal-data" json:"personal_data"`
	SpecialCategoryData TriState `yaml:"special-category-data" json:"special_category_data"`
	ChildrenData        TriState `yaml:"children-data" json:"children_data"`
}

type ProfileReview struct {
	Status     ReviewStatus `yaml:"status" json:"status"`
	ReviewedBy string       `yaml:"reviewed-by,omitempty" json:"reviewed_by,omitempty"`
	ReviewedAt string       `yaml:"reviewed-at,omitempty" json:"reviewed_at,omitempty"`
}

type ApplicabilityDecision struct {
	Framework  string              `yaml:"framework" json:"framework"`
	Status     ApplicabilityStatus `yaml:"status" json:"status"`
	Rationale  string              `yaml:"rationale,omitempty" json:"rationale,omitempty"`
	ReviewedBy string              `yaml:"reviewed-by,omitempty" json:"reviewed_by,omitempty"`
	ReviewedAt string              `yaml:"reviewed-at,omitempty" json:"reviewed_at,omitempty"`
}

func ValidateSystems(systems []System) error {
	seen := make(map[string]struct{}, len(systems))
	for index := range systems {
		if err := systems[index].Validate(); err != nil {
			return fmt.Errorf("systems[%d]: %w", index, err)
		}
		if _, exists := seen[systems[index].ID]; exists {
			return fmt.Errorf("systems[%d].id %q is duplicated", index, systems[index].ID)
		}
		seen[systems[index].ID] = struct{}{}
	}
	return nil
}

func (system System) Validate() error {
	if !systemIDPattern.MatchString(system.ID) {
		return errors.New("id must be 1-64 lowercase letters, numbers, dots, underscores, or hyphens and start with a letter or number")
	}
	if err := validatePlainText("name", system.Name, 200); err != nil {
		return err
	}
	if err := validatePlainText("intended-purpose", system.IntendedPurpose, 2000); err != nil {
		return fmt.Errorf("%w; use unknown when it has not been established", err)
	}
	if !oneOf(system.LifecycleStage, LifecycleDevelopment, LifecycleTesting, LifecycleProduction, LifecycleRetired, LifecycleUnknown) {
		return fmt.Errorf("lifecycle-stage %q is not supported", system.LifecycleStage)
	}
	if err := validateList("organization-roles", system.OrganizationRoles, []OrganizationRole{RoleProvider, RoleDeployer, RoleImporter, RoleDistributor, RoleProductManufacturer, RoleUnknown}); err != nil {
		return err
	}
	if len(system.OrganizationRoles) > 1 && oneOf(RoleUnknown, system.OrganizationRoles...) {
		return errors.New("organization-roles cannot combine unknown with established roles")
	}
	if err := validateList("operating-regions", system.OperatingRegions, []OperatingRegion{RegionEU, RegionEEA, RegionUK, RegionUS, RegionGlobal, RegionOther, RegionUnknown}); err != nil {
		return err
	}
	if len(system.OperatingRegions) > 1 && oneOf(RegionUnknown, system.OperatingRegions...) {
		return errors.New("operating-regions cannot combine unknown with established regions")
	}
	if err := validateList("use-case-domains", system.UseCaseDomains, []UseCaseDomain{DomainBiometrics, DomainCriticalInfrastructure, DomainEducation, DomainEmployment, DomainEssentialServices, DomainLawEnforcement, DomainMigrationBorderControl, DomainJusticeDemocraticProcess, DomainHealthcare, DomainSoftwareDevelopment, DomainGeneralPurpose, DomainOther, DomainUnknown}); err != nil {
		return err
	}
	if len(system.UseCaseDomains) > 1 && oneOf(DomainUnknown, system.UseCaseDomains...) {
		return errors.New("use-case-domains cannot combine unknown with established domains")
	}
	if err := validateTextList("users", system.Users); err != nil {
		return err
	}
	if len(system.Users) > 1 && containsText(system.Users, "unknown") {
		return errors.New("users cannot combine unknown with established users")
	}
	if err := validateTextList("affected-groups", system.AffectedGroups); err != nil {
		return err
	}
	if len(system.AffectedGroups) > 1 && containsText(system.AffectedGroups, "unknown") {
		return errors.New("affected-groups cannot combine unknown with established groups")
	}
	if !oneOf(system.DecisionImpact, ImpactAdvisory, ImpactLow, ImpactSignificant, ImpactAutonomous, ImpactUnknown) {
		return fmt.Errorf("decision-impact %q is not supported", system.DecisionImpact)
	}
	if !oneOf(system.HumanOversight, OversightRequired, OversightAvailable, OversightLimited, OversightNone, OversightUnknown) {
		return fmt.Errorf("human-oversight %q is not supported", system.HumanOversight)
	}
	for name, value := range map[string]TriState{
		"data.personal-data": system.Data.PersonalData, "data.special-category-data": system.Data.SpecialCategoryData, "data.children-data": system.Data.ChildrenData,
	} {
		if !oneOf(value, TriYes, TriNo, TriUnknown) {
			return fmt.Errorf("%s %q is not supported", name, value)
		}
	}
	if err := validateList("deployment-models", system.DeploymentModels, []DeploymentModel{DeploymentInternal, DeploymentPrivateCustomer, DeploymentPublic, DeploymentOpenSource, DeploymentEmbedded, DeploymentAPI, DeploymentLocalCLI, DeploymentUnknown}); err != nil {
		return err
	}
	if len(system.DeploymentModels) > 1 && oneOf(DeploymentUnknown, system.DeploymentModels...) {
		return errors.New("deployment-models cannot combine unknown with established models")
	}
	if err := system.ProfileReview.Validate(); err != nil {
		return fmt.Errorf("profile-review: %w", err)
	}
	seenFrameworks := make(map[string]struct{}, len(system.Applicability))
	for index, decision := range system.Applicability {
		if err := decision.Validate(); err != nil {
			return fmt.Errorf("applicability[%d]: %w", index, err)
		}
		if _, exists := seenFrameworks[decision.Framework]; exists {
			return fmt.Errorf("applicability[%d].framework %q is duplicated", index, decision.Framework)
		}
		seenFrameworks[decision.Framework] = struct{}{}
	}
	return nil
}

func (review ProfileReview) Validate() error {
	if !oneOf(review.Status, ReviewDraft, ReviewConfirmed) {
		return fmt.Errorf("status %q is not supported", review.Status)
	}
	if review.Status == ReviewConfirmed {
		if err := validatePlainText("reviewed-by", review.ReviewedBy, 200); err != nil {
			return fmt.Errorf("%w when status is confirmed", err)
		}
		if err := validDate(review.ReviewedAt); err != nil {
			return fmt.Errorf("reviewed-at: %w", err)
		}
	} else if review.ReviewedAt != "" {
		if err := validDate(review.ReviewedAt); err != nil {
			return fmt.Errorf("reviewed-at: %w", err)
		}
	}
	return nil
}

func (decision ApplicabilityDecision) Validate() error {
	if decision.Framework != FrameworkEUAIAct {
		return fmt.Errorf("framework %q is not supported", decision.Framework)
	}
	if !oneOf(decision.Status, ApplicabilityNeedsReview, ApplicabilityApplicable, ApplicabilityNotApplicable, ApplicabilityUncertain) {
		return fmt.Errorf("status %q is not supported", decision.Status)
	}
	if decision.Status == ApplicabilityNeedsReview {
		return nil
	}
	if err := validatePlainText("rationale", decision.Rationale, 2000); err != nil {
		return fmt.Errorf("%w for a recorded human decision", err)
	}
	if err := validatePlainText("reviewed-by", decision.ReviewedBy, 200); err != nil {
		return fmt.Errorf("%w for a recorded human decision", err)
	}
	if err := validDate(decision.ReviewedAt); err != nil {
		return fmt.Errorf("reviewed-at: %w", err)
	}
	return nil
}

func validDate(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("must be set to YYYY-MM-DD")
	}
	if _, err := time.Parse(time.DateOnly, value); err != nil {
		return errors.New("must use YYYY-MM-DD")
	}
	return nil
}

func validateTextList(name string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s must contain at least one value; use unknown when it has not been established", name)
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if err := validatePlainText(fmt.Sprintf("%s[%d]", name, index), value, 200); err != nil {
			return err
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%s[%d] %q is duplicated", name, index, value)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validatePlainText(name, value string, maximum int) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if len([]rune(value)) > maximum {
		return fmt.Errorf("%s must not exceed %d characters", name, maximum)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s must not contain control characters or line breaks", name)
		}
	}
	return nil
}

func validateList[T comparable](name string, values, allowed []T) error {
	if len(values) == 0 {
		return fmt.Errorf("%s must contain at least one value", name)
	}
	seen := make(map[T]struct{}, len(values))
	for index, value := range values {
		if !oneOf(value, allowed...) {
			return fmt.Errorf("%s[%d] %v is not supported", name, index, value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s[%d] %v is duplicated", name, index, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func oneOf[T comparable](value T, allowed ...T) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
