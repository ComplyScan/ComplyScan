package cli

import (
	"bufio"
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func TestOwnershipSetupWritesValidatedRulesAndShowReportsThem(t *testing.T) {
	target := t.TempDir()
	cfg := config.Default()
	cfg.Systems = []profile.System{
		profile.NewDraftSystem("ranking", "Ranking"),
		profile.NewDraftSystem("support", "Support"),
	}
	path := filepath.Join(target, config.FileName)
	if err := config.Write(path, cfg, false); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("services/ranking/**\nranking\ny\nshared/models/**\nranking,support\nn\n")
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{"ownership", "setup", "--interactive", target}, input, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ownership) != 2 || len(loaded.Ownership[1].Systems) != 2 {
		t.Fatalf("ownership = %#v", loaded.Ownership)
	}
	if !strings.Contains(stdout.String(), "Patterns use gitignore-style matching") || !strings.Contains(stdout.String(), "Saved 2 ownership rule(s)") {
		t.Fatalf("missing guided output:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = executeWithInput([]string{"ownership", "show", "--format", "json", target}, strings.NewReader(""), &stdout, &stderr, testBuild)
	if code != 0 || !strings.Contains(stdout.String(), `"services/ranking/**"`) || !strings.Contains(stdout.String(), `"support"`) {
		t.Fatalf("show exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
}

func TestOfferOwnershipSetupCanLeaveMultiSystemEvidenceUnassigned(t *testing.T) {
	cfg := config.Default()
	cfg.Systems = []profile.System{
		profile.NewDraftSystem("ranking", "Ranking"),
		profile.NewDraftSystem("support", "Support"),
	}
	var output bytes.Buffer
	prompt := promptSession{reader: bufferedInput("n\n"), output: &output}
	if err := offerOwnershipSetup(prompt, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Ownership) != 0 || !strings.Contains(output.String(), "Evidence will remain unassigned") {
		t.Fatalf("unexpected ownership offer result: %#v\n%s", cfg.Ownership, output.String())
	}
}

func TestCollectOwnershipRulesDoesNotReplaceExistingRulesWithoutConfirmation(t *testing.T) {
	cfg := config.Default()
	cfg.Systems = []profile.System{profile.NewDraftSystem("ranking", "Ranking")}
	cfg.Ownership = []ownership.Rule{{Paths: []string{"existing/**"}, Systems: []string{"ranking"}}}
	var output bytes.Buffer
	prompt := promptSession{reader: bufferedInput("n\n"), output: &output}
	changed, err := collectOwnershipRules(prompt, &cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed || cfg.Ownership[0].Paths[0] != "existing/**" {
		t.Fatalf("existing ownership changed: %#v", cfg.Ownership)
	}
}

func bufferedInput(value string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(value))
}
