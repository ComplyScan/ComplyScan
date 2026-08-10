package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/spf13/cobra"
)

const doctorHTTPResponseLimit = 1 << 20

var doctorHTTPClient = newDoctorHTTPClient()

type doctorOutput struct {
	writer   io.Writer
	failures int
}

type ollamaModelRecord struct {
	Name   string `json:"name"`
	Model  string `json:"model"`
	Digest string `json:"digest"`
}

func newDoctorCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	var configPath string
	var probeReview bool
	command := &cobra.Command{
		Use:   "doctor [path]",
		Short: "Check whether ComplyScan and its optional reviewer are ready",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			return runDoctor(cmd.Context(), stdout, build, target, configPath, probeReview)
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().BoolVar(&probeReview, "probe-review", false, "send a small synthetic structured-output request to the configured reviewer (remote providers may charge)")
	return command
}

func runDoctor(ctx context.Context, stdout io.Writer, build BuildInfo, target, configPath string, probeReview bool) error {
	output := doctorOutput{writer: stdout}
	if err := output.write("PASS", "version", fmt.Sprintf("ComplyScan %s (commit %s, built %s)", build.Version, build.Commit, build.BuildDate)); err != nil {
		return err
	}

	targetPath, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve doctor target %q: %w", target, err)
	}
	info, err := os.Stat(targetPath)
	if err != nil || !info.IsDir() {
		detail := "not a directory"
		if err != nil {
			detail = err.Error()
		}
		if writeErr := output.write("FAIL", "target", fmt.Sprintf("%s: %s", targetPath, detail)); writeErr != nil {
			return writeErr
		}
		return output.finish()
	}
	if err := output.write("PASS", "target", targetPath); err != nil {
		return err
	}

	cfg, resolvedConfig, configErr := config.Resolve(targetPath, configPath)
	if configErr != nil {
		if err := output.write("FAIL", "config", configErr.Error()); err != nil {
			return err
		}
	} else if resolvedConfig == "" {
		if err := output.write("WARN", "config", "no .complyscan.yml found; built-in defaults are valid"); err != nil {
			return err
		}
	} else if err := output.write("PASS", "config", resolvedConfig); err != nil {
		return err
	}

	gitRoot, gitErr := detectGitRoot(ctx, targetPath)
	if gitErr != nil {
		if err := output.write("WARN", "git", gitErr.Error()); err != nil {
			return err
		}
	} else if err := output.write("PASS", "git", gitRoot); err != nil {
		return err
	}

	reportPath := filepath.Join(targetPath, filepath.FromSlash(report.DefaultDirectory))
	if err := probeReportDestination(targetPath); err != nil {
		if writeErr := output.write("FAIL", "reports", err.Error()); writeErr != nil {
			return writeErr
		}
	} else if err := output.write("PASS", "reports", "writable at "+reportPath); err != nil {
		return err
	}

	ollamaPath, ollamaErr := exec.LookPath("ollama")
	providerRequired := configErr == nil && cfg.AI.Provider == "ollama"
	if ollamaErr != nil {
		status := "WARN"
		if providerRequired {
			status = "FAIL"
		}
		if err := output.write(status, "ollama executable", "not found on PATH"); err != nil {
			return err
		}
	} else if err := output.write("PASS", "ollama executable", ollamaPath); err != nil {
		return err
	}

	if configErr != nil {
		if err := output.write("SKIP", "ollama service", "configuration is invalid"); err != nil {
			return err
		}
	} else if cfg.AI.Provider != "ollama" {
		detail := "AI review is disabled in configuration"
		if cfg.AI.Provider != "none" {
			detail = reviewProviderLabel(cfg.AI.Provider) + " review does not use Ollama"
		}
		if err := output.write("SKIP", "ollama service", detail); err != nil {
			return err
		}
	} else {
		models, serviceErr := fetchOllamaModels(ctx, cfg.AI.Ollama.Endpoint)
		if serviceErr != nil {
			if err := output.write("FAIL", "ollama service", serviceErr.Error()); err != nil {
				return err
			}
			if err := output.write("SKIP", "ollama model", "service is unavailable"); err != nil {
				return err
			}
		} else {
			if err := output.write("PASS", "ollama service", cfg.AI.Ollama.Endpoint); err != nil {
				return err
			}
			if containsModel(models, cfg.AI.Ollama.Model) {
				if err := output.write("PASS", "ollama model", cfg.AI.Ollama.Model); err != nil {
					return err
				}
			} else if err := output.write("FAIL", "ollama model", fmt.Sprintf("%s is not installed; run: ollama pull %s", cfg.AI.Ollama.Model, shellQuote(cfg.AI.Ollama.Model))); err != nil {
				return err
			}
		}
	}

	if configErr != nil {
		if err := output.write("SKIP", "remote credential", "configuration is invalid"); err != nil {
			return err
		}
	} else if !isRemoteReviewProvider(cfg.AI.Provider) {
		if err := output.write("SKIP", "remote credential", "no remote review provider is configured"); err != nil {
			return err
		}
	} else {
		value, exists := os.LookupEnv(cfg.AI.Remote.APIKeyEnv)
		if !exists || strings.TrimSpace(value) == "" {
			if err := output.write("FAIL", "remote credential", cfg.AI.Remote.APIKeyEnv+" is not set"); err != nil {
				return err
			}
		} else if err := output.write("PASS", "remote credential", cfg.AI.Remote.APIKeyEnv+" is set (value hidden)"); err != nil {
			return err
		}
		if err := output.write("PASS", "remote model", fmt.Sprintf("%s via %s (qualification status reported below)", cfg.AI.Remote.Model, reviewProviderLabel(cfg.AI.Provider))); err != nil {
			return err
		}
	}

	if probeReview {
		if configErr != nil {
			if err := output.write("SKIP", "review compatibility", "configuration is invalid"); err != nil {
				return err
			}
		} else if cfg.AI.Provider == "none" {
			if err := output.write("FAIL", "review compatibility", "no advisory review provider is configured"); err != nil {
				return err
			}
		} else {
			outcome, qualificationErr := qualifyConfiguredModel(ctx, cfg.AI, true)
			if qualificationErr != nil {
				if writeErr := output.write("FAIL", "review compatibility", qualificationErr.Error()); writeErr != nil {
					return writeErr
				}
			} else if err := output.write("PASS", "review compatibility", fmt.Sprintf("compatible; checked with synthetic input and cached until %s", outcome.Result.ExpiresAt.Format("2006-01-02"))); err != nil {
				return err
			}
			if qualificationErr == nil && outcome.CacheWarning != nil {
				if err := output.write("WARN", "qualification cache", outcome.CacheWarning.Error()); err != nil {
					return err
				}
			}
		}
	} else if configErr == nil && cfg.AI.Provider != "none" {
		outcome, found, lookupErr := lookupConfiguredQualification(ctx, cfg.AI)
		if lookupErr != nil {
			if err := output.write("WARN", "review compatibility", "cached status unavailable: "+lookupErr.Error()+"; run doctor --probe-review"); err != nil {
				return err
			}
		} else if found {
			if err := output.write("PASS", "review compatibility", fmt.Sprintf("compatible; cached check valid until %s", outcome.Result.ExpiresAt.Format("2006-01-02"))); err != nil {
				return err
			}
		} else if err := output.write("WARN", "review compatibility", "model has not passed the current automatic contract; run doctor --probe-review"); err != nil {
			return err
		}
	} else if err := output.write("SKIP", "review compatibility", "no advisory review provider is configured"); err != nil {
		return err
	}

	return output.finish()
}

func (output *doctorOutput) write(status, name, detail string) error {
	if status == "FAIL" {
		output.failures++
	}
	_, err := fmt.Fprintf(output.writer, "[%s] %s: %s\n", status, name, detail)
	return err
}

func (output doctorOutput) finish() error {
	if output.failures > 0 {
		if _, err := fmt.Fprintf(output.writer, "\nDoctor found %d blocking issue(s).\n", output.failures); err != nil {
			return err
		}
		return &exitError{code: 1}
	}
	_, err := fmt.Fprintln(output.writer, "\nDoctor found no blocking issues.")
	return err
}

func detectGitRoot(ctx context.Context, target string) (string, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return "", errors.New("git is not installed; tracked-only and changed-since scans are unavailable")
	}
	command := exec.CommandContext(ctx, gitPath, "-C", target, "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err != nil {
		return "", errors.New("target is not inside a Git repository; full repository scans remain available")
	}
	return strings.TrimSpace(string(output)), nil
}

func probeReportDestination(target string) error {
	probe := target
	for _, path := range []string{
		filepath.Join(target, ".complyscan"),
		filepath.Join(target, filepath.FromSlash(report.DefaultDirectory)),
	} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return fmt.Errorf("inspect report path %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("report path %q must be a directory and must not be a symlink", path)
		}
		probe = path
	}
	temporary, err := os.CreateTemp(probe, ".complyscan-doctor-*")
	if err != nil {
		return fmt.Errorf("report parent %q is not writable: %w", probe, err)
	}
	name := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("close report permission probe: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("remove report permission probe: %w", err)
	}
	return nil
}

func fetchOllamaModels(ctx context.Context, endpoint string) ([]string, error) {
	records, err := fetchOllamaModelRecords(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(records)*2)
	for _, model := range records {
		models = append(models, model.Name, model.Model)
	}
	return models, nil
}

func fetchOllamaModelRecords(ctx context.Context, endpoint string) ([]ollamaModelRecord, error) {
	tagsURL, err := ollamaTagsURL(endpoint)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create Ollama readiness request: %w", err)
	}
	response, err := doctorHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", endpoint, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned HTTP %d", tagsURL, response.StatusCode)
	}
	var payload struct {
		Models []ollamaModelRecord `json:"models"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, doctorHTTPResponseLimit))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode %s: %w", tagsURL, err)
	}
	return payload.Models, nil
}

func newDoctorHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func ollamaTagsURL(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse Ollama endpoint: %w", err)
	}
	parsed.Path = "/api/tags"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func containsModel(models []string, wanted string) bool {
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	for _, model := range models {
		if strings.ToLower(strings.TrimSpace(model)) == wanted {
			return true
		}
	}
	return false
}
