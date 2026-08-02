package profile

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
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
	if strings.TrimSpace(system.Name) == "" {
		return errors.New("name must not be empty")
	}
	if strings.TrimSpace(system.IntendedPurpose) == "" {
		return errors.New("intended-purpose must not be empty; use unknown when it has not been established")
	}
	if !oneOf(system.LifecycleStage, LifecycleDevelopment, LifecycleTesting, LifecycleProduction, LifecycleRetired, LifecycleUnknown) {
		return fmt.Errorf("lifecycle-stage %q is not supported", system.LifecycleStage)
	}
	if err := validateList("organization-roles", system.OrganizationRoles, []OrganizationRole{RoleProvider, RoleDeployer, RoleImporter, RoleDistributor, RoleProductManufacturer, RoleUnknown}); err != nil {
		return err
	}
	if err := validateList("operating-regions", system.OperatingRegions, []OperatingRegion{RegionEU, RegionEEA, RegionUK, RegionUS, RegionGlobal, RegionOther, RegionUnknown}); err != nil {
		return err
	}
	if err := validateList("use-case-domains", system.UseCaseDomains, []UseCaseDomain{DomainBiometrics, DomainCriticalInfrastructure, DomainEducation, DomainEmployment, DomainEssentialServices, DomainLawEnforcement, DomainMigrationBorderControl, DomainJusticeDemocraticProcess, DomainHealthcare, DomainSoftwareDevelopment, DomainGeneralPurpose, DomainOther, DomainUnknown}); err != nil {
		return err
	}
	if err := validateTextList("users", system.Users); err != nil {
		return err
	}
	if err := validateTextList("affected-groups", system.AffectedGroups); err != nil {
		return err
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
		if strings.TrimSpace(review.ReviewedBy) == "" {
			return errors.New("reviewed-by is required when status is confirmed")
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
	if strings.TrimSpace(decision.Rationale) == "" || strings.TrimSpace(decision.ReviewedBy) == "" {
		return errors.New("rationale and reviewed-by are required for a recorded human decision")
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
		if value == "" {
			return fmt.Errorf("%s[%d] must not be empty", name, index)
		}
		if len(value) > 200 {
			return fmt.Errorf("%s[%d] must not exceed 200 characters", name, index)
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%s[%d] %q is duplicated", name, index, value)
		}
		seen[key] = struct{}{}
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
