package cli

import (
	"context"
	"crypto/sha256"
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
	modelQualificationReviewer    = configuredModelQualificationReviewer
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
	reviewer, timeout, err := modelQualificationReviewer(settings)
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
		return modelQualificationOutcome{Result: result, CacheWarning: cacheWarning}, err
	}
	if cache != nil {
		if storeErr := cache.Store(result); storeErr != nil {
			cacheWarning = storeErr
		}
	}
	return modelQualificationOutcome{Result: result, CacheWarning: cacheWarning}, nil
}

func configuredModelQualificationReviewer(settings config.AIConfig) (modelqualification.Reviewer, time.Duration, error) {
	reviewer, timeout, _, _, _, err := configuredReviewer(settings)
	return reviewer, timeout, err
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
	} else if isOpenAICompatibleProvider(settings.Provider) {
		digest = fmt.Sprintf("endpoint-sha256:%x", sha256.Sum256([]byte(strings.TrimRight(strings.TrimSpace(settings.Remote.BaseURL), "/"))))
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
	if _, err := fmt.Fprintf(output, "Checking %s model %q with bounded synthetic finding and repository compatibility requests (no repository data; at most %d provider requests including retries)...\n", reviewProviderLabel(settings.Provider), configuredReviewModel(settings), modelqualification.MaximumProviderRequests); err != nil {
		return false, err
	}
	qualificationStarted := time.Now()
	activity := startConfiguredLLMActivity(output, settings, "check compatibility", "Compatibility response received", "Compatibility request failed")
	outcome, err := qualifyConfiguredModel(ctx, settings, false)
	activity.Finish(err)
	if err != nil {
		accounting := qualificationRunAccounting(outcome.Result)
		if accounting != "" {
			accounting = "\nCompatibility attempt accounting: " + accounting + "."
		}
		if _, writeErr := fmt.Fprintf(output, "Model qualification failed after %s: %v%s\nDeterministic setup will continue; choose another model or retry with `complyscan doctor --probe-review`.\n", formatElapsed(time.Since(qualificationStarted)), err, accounting); writeErr != nil {
			return false, writeErr
		}
		return false, nil
	}
	source := "live check"
	if outcome.Result.FromCache {
		source = "cached check"
	} else if outcome.Result.ProviderRequests > 0 {
		source = "live check; " + qualificationRunAccounting(outcome.Result)
	}
	if _, err := fmt.Fprintf(output, "Model status: compatible in %s (%s; expires %s). This checks the ComplyScan contract, not model accuracy or legal correctness.\n", formatElapsed(time.Since(qualificationStarted)), source, outcome.Result.ExpiresAt.Format("2006-01-02")); err != nil {
		return false, err
	}
	if outcome.CacheWarning != nil {
		if _, err := fmt.Fprintf(output, "Warning: compatibility passed but its private cache could not be updated: %v\n", outcome.CacheWarning); err != nil {
			return false, err
		}
	}
	return true, nil
}

func qualificationRunAccounting(result modelqualification.Result) string {
	if result.ProviderRequests <= 0 {
		return ""
	}
	return fmt.Sprintf("%d provider request(s), %d input / %d output / %d reasoning token(s)",
		result.ProviderRequests, result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.ReasoningTokens)
}
