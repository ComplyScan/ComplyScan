package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/profiledraft"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type caseList []string

func (values *caseList) String() string { return strings.Join(*values, ",") }

func (values *caseList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("case ID must not be empty")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	os.Exit(run())
}

func run() int {
	manifestPath := flag.String("manifest", "testdata/profile-draft-evaluation/manifest.json", "path to the labelled profile-draft manifest")
	providerName := flag.String("provider", "ollama", "review provider: ollama, openai, anthropic, or gemini")
	model := flag.String("model", "qwen3.5:9b", "exact model ID to evaluate")
	endpoint := flag.String("endpoint", "http://127.0.0.1:11434", "Ollama endpoint")
	apiKeyEnvironment := flag.String("api-key-env", "", "environment-variable name containing the hosted-provider API key")
	output := flag.String("output", "", "optional path for the complete JSON result")
	var selectedCases caseList
	flag.Var(&selectedCases, "case", "case ID to run; repeat to select multiple cases")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Usage: evaluate-profile-draft [--manifest PATH] [--provider PROVIDER] [--model MODEL] [--endpoint URL] [--api-key-env NAME] [--case ID] [--output PATH]")
		return 2
	}
	resolvedManifest, err := filepath.Abs(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: resolve manifest: %v\n", err)
		return 2
	}
	manifest, err := profiledraft.LoadBenchmarkManifest(resolvedManifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	if len(selectedCases) > 0 {
		manifest.Cases, err = filterCases(manifest.Cases, selectedCases)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 2
		}
	}
	provider, err := newProfileDraftProvider(*providerName, *model, *endpoint, *apiKeyEnvironment, time.Duration(manifest.Acceptance.MaximumCaseSeconds+30)*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	report, err := profiledraft.RunBenchmarkWithProgress(context.Background(), resolvedManifest, manifest, *model, provider, writeProgress)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	if err := profiledraft.WriteBenchmarkSummary(os.Stdout, report); err != nil {
		fmt.Fprintf(os.Stderr, "Error: write benchmark summary: %v\n", err)
		return 2
	}
	if strings.TrimSpace(*output) != "" {
		if err := writeJSON(*output, report); err != nil {
			fmt.Fprintf(os.Stderr, "Error: write benchmark result: %v\n", err)
			return 2
		}
	}
	if !report.Passed {
		return 1
	}
	return 0
}

func newProfileDraftProvider(providerName, model, endpoint, apiKeyEnvironment string, timeout time.Duration) (profiledraft.Drafter, error) {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, errors.New("model ID must not be empty")
	}
	if providerName == "ollama" {
		return providers.NewOllama(providers.OllamaOptions{
			Endpoint: endpoint, Model: model, Timeout: timeout, MaxFindings: 1,
		})
	}
	if providerName != "openai" && providerName != "anthropic" && providerName != "gemini" {
		return nil, fmt.Errorf("unsupported profile-draft provider %q (choose ollama, openai, anthropic, or gemini)", providerName)
	}
	apiKeyEnvironment = strings.TrimSpace(apiKeyEnvironment)
	if apiKeyEnvironment == "" || strings.ContainsAny(apiKeyEnvironment, "\r\n\x00=") {
		return nil, errors.New("hosted-provider evaluation requires a valid --api-key-env name")
	}
	apiKey := strings.TrimSpace(os.Getenv(apiKeyEnvironment))
	if apiKey == "" {
		return nil, fmt.Errorf("%s is not set; the evaluator never accepts or stores an API key value", apiKeyEnvironment)
	}
	options := providers.RemoteOptions{APIKey: apiKey, Model: model, Timeout: timeout, MaxFindings: 1}
	switch providerName {
	case "openai":
		return providers.NewOpenAI(options)
	case "anthropic":
		return providers.NewAnthropic(options)
	case "gemini":
		return providers.NewGemini(options)
	}
	return nil, errors.New("unreachable profile-draft provider")
}

func writeProgress(progress profiledraft.BenchmarkProgress) {
	if !progress.Done {
		fmt.Fprintf(os.Stderr, "[%d/%d] Drafting %s...\n", progress.Index, progress.Total, progress.CaseID)
		return
	}
	status := "done"
	if progress.Err != nil {
		status = "error: " + progress.Err.Error()
	}
	fmt.Fprintf(os.Stderr, "[%d/%d] %s %s (%d ms)\n", progress.Index, progress.Total, progress.CaseID, status, progress.DurationMS)
}

func filterCases(cases []profiledraft.BenchmarkCase, selected []string) ([]profiledraft.BenchmarkCase, error) {
	wanted := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		wanted[id] = struct{}{}
	}
	result := make([]profiledraft.BenchmarkCase, 0, len(wanted))
	for _, benchmarkCase := range cases {
		if _, exists := wanted[benchmarkCase.ID]; exists {
			result = append(result, benchmarkCase)
			delete(wanted, benchmarkCase.ID)
		}
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("unknown benchmark case(s): %s", strings.Join(missing, ", "))
	}
	return result, nil
}

func writeJSON(path string, report profiledraft.BenchmarkReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}
