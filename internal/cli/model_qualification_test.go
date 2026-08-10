package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/modelqualification"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestFinishSetupModelQualificationUsesAutomaticCompatibleResult(t *testing.T) {
	previous := qualifyConfiguredModel
	qualifyConfiguredModel = func(context.Context, config.AIConfig, bool) (modelQualificationOutcome, error) {
		return modelQualificationOutcome{Result: modelqualification.Result{
			Status: "compatible", FromCache: true, ExpiresAt: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
		}}, nil
	}
	t.Cleanup(func() { qualifyConfiguredModel = previous })
	settings := config.Default().AI
	settings.Provider = "ollama"
	var output bytes.Buffer
	ready, err := finishSetupModelQualification(context.Background(), &output, settings, true)
	if err != nil || !ready {
		t.Fatalf("ready=%t err=%v output=%s", ready, err, output.String())
	}
	for _, expected := range []string{"small synthetic compatibility request", "Model status: compatible", "cached check", "not model accuracy or legal correctness"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestFinishSetupModelQualificationFallsBackDeterministically(t *testing.T) {
	previous := qualifyConfiguredModel
	qualifyConfiguredModel = func(context.Context, config.AIConfig, bool) (modelQualificationOutcome, error) {
		return modelQualificationOutcome{}, errors.New("schema unsupported")
	}
	t.Cleanup(func() { qualifyConfiguredModel = previous })
	settings := config.Default().AI
	settings.Provider = "ollama"
	var output bytes.Buffer
	ready, err := finishSetupModelQualification(context.Background(), &output, settings, true)
	if err != nil || ready {
		t.Fatalf("ready=%t err=%v output=%s", ready, err, output.String())
	}
	if !strings.Contains(output.String(), "Deterministic setup will continue") {
		t.Fatalf("fallback output:\n%s", output.String())
	}
}

func TestFinishSetupModelQualificationSkipsImplicitAutomationRequest(t *testing.T) {
	previous := qualifyConfiguredModel
	called := false
	qualifyConfiguredModel = func(context.Context, config.AIConfig, bool) (modelQualificationOutcome, error) {
		called = true
		return modelQualificationOutcome{}, nil
	}
	t.Cleanup(func() { qualifyConfiguredModel = previous })
	settings := config.Default().AI
	settings.Provider = "openai"
	settings.Remote.Model = "test"
	var output bytes.Buffer
	ready, err := finishSetupModelQualification(context.Background(), &output, settings, false)
	if err != nil || !ready || called {
		t.Fatalf("ready=%t called=%t err=%v", ready, called, err)
	}
	if !strings.Contains(output.String(), "--qualify-model") {
		t.Fatalf("skip output:\n%s", output.String())
	}
}

func TestConfiguredQualificationIdentityIncludesOllamaDigest(t *testing.T) {
	useDoctorHTTPTransport(t, func(*http.Request) (*http.Response, error) {
		return doctorHTTPResponse(200, `{"models":[{"name":"qwen3.5:9b","model":"qwen3.5:9b","digest":"sha256:abc"}]}`), nil
	})
	settings := config.Default().AI
	settings.Provider = "ollama"
	identity, err := configuredQualificationIdentity(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Provider != providers.Ollama || identity.ModelDigest != "sha256:abc" {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestDoctorProbeRefreshesAutomaticQualification(t *testing.T) {
	target := t.TempDir()
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote = config.RemoteConfig{
		Model: "account-model", APIKeyEnv: "COMPLYSCAN_QUALIFICATION_TEST_KEY", TimeoutSeconds: 60, MaxFindings: 1,
	}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPLYSCAN_QUALIFICATION_TEST_KEY", "secret-value")
	previous := qualifyConfiguredModel
	qualifyConfiguredModel = func(_ context.Context, actual config.AIConfig, refresh bool) (modelQualificationOutcome, error) {
		if actual.Remote.Model != "account-model" || !refresh {
			t.Fatalf("settings=%#v refresh=%t", actual, refresh)
		}
		return modelQualificationOutcome{Result: modelqualification.Result{
			Status: "compatible", ExpiresAt: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
		}}, nil
	}
	t.Cleanup(func() { qualifyConfiguredModel = previous })
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"doctor", "--probe-review", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[PASS] review compatibility: compatible; checked with synthetic input and cached until 2026-09-09") {
		t.Fatalf("doctor output:\n%s", stdout.String())
	}
}
