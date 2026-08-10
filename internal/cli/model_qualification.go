package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/modelqualification"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

const maximumQualificationTimeout = 2 * time.Minute

type modelQualificationOutcome struct {
	Result       modelqualification.Result
	CacheWarning error
}

var (
	modelQualificationDefaultPath = modelqualification.DefaultPath
	modelQualificationNow         = time.Now
	lookupConfiguredQualification = lookupModelQualification
	qualifyConfiguredModel        = runModelQualification
)

func lookupModelQualification(ctx context.Context, settings config.AIConfig) (modelQualificationOutcome, bool, error) {
	identity, err := configuredQualificationIdentity(ctx, settings)
	if err != nil {
		return modelQualificationOutcome{}, false, err
	}
	path, err := modelQualificationDefaultPath()
	if err != nil {
		return modelQualificationOutcome{}, false, err
	}
	cache, err := modelqualification.Open(path)
	if err != nil {
		return modelQualificationOutcome{}, false, err
	}
	result, found, err := cache.Lookup(identity, modelQualificationNow())
	return modelQualificationOutcome{Result: result}, found, err
}

func runModelQualification(ctx context.Context, settings config.AIConfig, refresh bool) (modelQualificationOutcome, error) {
	identity, err := configuredQualificationIdentity(ctx, settings)
	if err != nil {
		return modelQualificationOutcome{}, err
	}
	var cache *modelqualification.Cache
	var cacheWarning error
	if path, pathErr := modelQualificationDefaultPath(); pathErr != nil {
		cacheWarning = pathErr
	} else if opened, openErr := modelqualification.Open(path); openErr != nil {
		cacheWarning = openErr
	} else {
		cache = opened
	}
	if cache != nil && !refresh {
		if result, found, lookupErr := cache.Lookup(identity, modelQualificationNow()); lookupErr != nil {
			cacheWarning = lookupErr
		} else if found {
			return modelQualificationOutcome{Result: result, CacheWarning: cacheWarning}, nil
		}
	}
	reviewer, timeout, _, _, _, err := configuredReviewer(settings)
	if err != nil {
		return modelQualificationOutcome{}, err
	}
	if timeout > maximumQualificationTimeout {
		timeout = maximumQualificationTimeout
	}
	probeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := modelqualification.Qualify(probeContext, reviewer, identity, modelQualificationNow())
	if err != nil {
		return modelQualificationOutcome{}, err
	}
	if cache != nil {
		if storeErr := cache.Store(result); storeErr != nil {
			cacheWarning = storeErr
		}
	}
	return modelQualificationOutcome{Result: result, CacheWarning: cacheWarning}, nil
}

func configuredQualificationIdentity(ctx context.Context, settings config.AIConfig) (modelqualification.Identity, error) {
	model := strings.TrimSpace(configuredReviewModel(settings))
	if model == "" || settings.Provider == "none" {
		return modelqualification.Identity{}, fmt.Errorf("no advisory model is configured")
	}
	kind := providers.Kind(settings.Provider)
	digest := ""
	if settings.Provider == "ollama" {
		if records, err := fetchOllamaModelRecords(ctx, settings.Ollama.Endpoint); err == nil {
			for _, record := range records {
				if strings.EqualFold(record.Name, model) || strings.EqualFold(record.Model, model) {
					digest = record.Digest
					break
				}
			}
		}
	}
	return modelqualification.CurrentIdentity(kind, model, digest), nil
}

func finishSetupModelQualification(ctx context.Context, output interface {
	Write([]byte) (int, error)
}, settings config.AIConfig, automatic bool) (bool, error) {
	if !automatic {
		_, err := fmt.Fprintln(output, "Model configured but not contacted during non-interactive setup. Run `complyscan doctor --probe-review` or pass `--qualify-model` to check it automatically.")
		return true, err
	}
	if _, err := fmt.Fprintf(output, "Checking %s model %q with a small synthetic compatibility request (no repository data)...\n", reviewProviderLabel(settings.Provider), configuredReviewModel(settings)); err != nil {
		return false, err
	}
	outcome, err := qualifyConfiguredModel(ctx, settings, false)
	if err != nil {
		if _, writeErr := fmt.Fprintf(output, "Model qualification failed: %v\nDeterministic setup will continue; choose another model or retry with `complyscan doctor --probe-review`.\n", err); writeErr != nil {
			return false, writeErr
		}
		return false, nil
	}
	source := "live check"
	if outcome.Result.FromCache {
		source = "cached check"
	}
	if _, err := fmt.Fprintf(output, "Model status: compatible (%s; expires %s). This checks the ComplyScan contract, not model accuracy or legal correctness.\n", source, outcome.Result.ExpiresAt.Format("2006-01-02")); err != nil {
		return false, err
	}
	if outcome.CacheWarning != nil {
		if _, err := fmt.Fprintf(output, "Warning: compatibility passed but its private cache could not be updated: %v\n", outcome.CacheWarning); err != nil {
			return false, err
		}
	}
	return true, nil
}
