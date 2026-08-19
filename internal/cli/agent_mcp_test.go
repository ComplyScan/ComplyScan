package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

func TestAgentMCPListsReadOnlyToolsAndDeveloperActions(t *testing.T) {
	target := t.TempDir()
	value := report.New(target, "test", []rules.Finding{{
		Fingerprint: "stable", RuleID: "AI-TEST-001", Severity: rules.SeverityHigh,
		Title: "Unsafe AI path", Message: "The path needs a guard.", Remediation: "Add the guard.", Path: "app.go", StartLine: 4,
	}}, nil, 0)
	value = report.ReconcileDeveloperActionLifecycle(value, nil)
	if _, err := report.WriteArtifacts(filepath.Join(target, report.DefaultDirectory), value); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"complyscan_list_actions","arguments":{"status":"active"}}}`,
	}, "\n") + "\n"
	var output, diagnostics bytes.Buffer
	if err := serveAgentMCP(strings.NewReader(input), &output, &diagnostics, target, testBuild); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("MCP responses = %d:\n%s", len(lines), output.String())
	}
	for index, line := range lines {
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("response %d: %v", index, err)
		}
		if response["error"] != nil {
			t.Fatalf("response %d = %#v", index, response)
		}
	}
	for _, fragment := range []string{"complyscan_scan_changed_code", "complyscan_verify_action", "finding/stable", "structuredContent"} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("MCP output missing %q:\n%s", fragment, output.String())
		}
	}
}
