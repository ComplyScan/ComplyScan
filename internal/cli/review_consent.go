package cli

import (
	"fmt"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/reviewconsent"
)

var defaultReviewConsentStore = reviewconsent.DefaultStore

func automaticReviewAuthorized(target, configPath string, settings config.AIConfig) (bool, error) {
	store, err := defaultReviewConsentStore()
	if err != nil {
		return false, err
	}
	return store.Authorized(target, configPath, settings)
}

func syncAutomaticReviewConsent(target, configPath string, settings config.AIConfig) error {
	store, err := defaultReviewConsentStore()
	if err != nil {
		return err
	}
	if settings.Provider == "none" || !settings.ReviewOnScan {
		return store.Revoke(target, configPath)
	}
	if err := store.Grant(target, configPath, settings); err != nil {
		return err
	}
	return nil
}

func automaticReviewConsentSaveError(configPath string, err error) error {
	return fmt.Errorf("saved %s, but could not record private machine-local AI review consent: %w; automatic AI review will remain disabled and scans will keep their deterministic results until `complyscan setup` succeeds", configPath, err)
}
