package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/ComplyScan/ComplyScan/internal/rules"
	"github.com/spf13/cobra"
)

const complyScanMCPProtocolVersion = "2025-06-18"

type agentMCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type agentMCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *agentMCPError  `json:"error,omitempty"`
}

type agentMCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type agentMCPToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func newAgentServeCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	command := &cobra.Command{
		Use:   "serve [path]",
		Short: "Serve read-only ComplyScan tools over MCP stdio",
		Long:  "Serve machine-readable list, evidence, changed-code scan, and deterministic verification tools over MCP stdio. The server never enables a model and never writes reports.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return serveAgentMCP(cmd.InOrStdin(), stdout, cmd.ErrOrStderr(), optionalTarget(args), build)
		},
	}
	return command
}

func serveAgentMCP(input io.Reader, output, diagnostics io.Writer, target string, build BuildInfo) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var request agentMCPRequest
		if err := json.Unmarshal(line, &request); err != nil {
			if err := encoder.Encode(agentMCPResponse{JSONRPC: "2.0", Error: &agentMCPError{Code: -32700, Message: "invalid JSON-RPC request"}}); err != nil {
				return err
			}
			continue
		}
		response, respond := handleAgentMCPRequest(request, target, build, diagnostics)
		if !respond {
			continue
		}
		if err := encoder.Encode(response); err != nil {
			return fmt.Errorf("write MCP response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read MCP request: %w", err)
	}
	return nil
}

func handleAgentMCPRequest(request agentMCPRequest, target string, build BuildInfo, diagnostics io.Writer) (agentMCPResponse, bool) {
	response := agentMCPResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "notifications/initialized", "notifications/cancelled":
		return agentMCPResponse{}, false
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": complyScanMCPProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]string{"name": "complyscan", "version": build.Version},
		}
	case "ping":
		response.Result = map[string]any{}
	case "tools/list":
		response.Result = map[string]any{"tools": agentMCPTools()}
	case "tools/call":
		var call agentMCPToolCall
		if err := json.Unmarshal(request.Params, &call); err != nil {
			response.Error = &agentMCPError{Code: -32602, Message: "invalid tools/call parameters"}
			break
		}
		result, err := callAgentMCPTool(call, target, build, diagnostics)
		if err != nil {
			response.Result = agentMCPToolError(err)
		} else {
			response.Result = agentMCPToolResult(result)
		}
	default:
		response.Error = &agentMCPError{Code: -32601, Message: "method not found"}
	}
	return response, true
}

func agentMCPTools() []map[string]any {
	object := func(properties map[string]any, required ...string) map[string]any {
		value := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			value["required"] = required
		}
		return value
	}
	return []map[string]any{
		{
			"name": "complyscan_list_actions", "description": "List active or historical developer actions from the latest local ComplyScan evidence bundle without scanning or contacting a model.",
			"inputSchema": object(map[string]any{"status": map[string]any{"type": "string", "enum": []string{"active", "all", "new", "open", "reopened", "resolved", "accepted"}}, "report": map[string]any{"type": "string"}}),
		},
		{
			"name": "complyscan_get_action", "description": "Get one stable action with its evidence, framework mapping, recommended change, and acceptance criteria.",
			"inputSchema": object(map[string]any{"id": map[string]any{"type": "string", "minLength": 1}, "report": map[string]any{"type": "string"}}, "id"),
		},
		{
			"name": "complyscan_scan_changed_code", "description": "Run a read-only deterministic scan of changed code. This never enables AI review and never writes report artifacts.",
			"inputSchema": object(map[string]any{"changed_since": map[string]any{"type": "string", "minLength": 1}, "severity": map[string]any{"type": "string", "enum": []string{"info", "low", "medium", "high", "critical"}}}, "changed_since"),
		},
		{
			"name": "complyscan_verify_action", "description": "Re-run local deterministic checks for one deterministic finding action without changing files, enabling a model, or writing reports.",
			"inputSchema": object(map[string]any{"id": map[string]any{"type": "string", "minLength": 1}, "report": map[string]any{"type": "string"}}, "id"),
		},
	}
}

func callAgentMCPTool(call agentMCPToolCall, target string, build BuildInfo, diagnostics io.Writer) (any, error) {
	var arguments struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		Report       string `json:"report"`
		ChangedSince string `json:"changed_since"`
		Severity     string `json:"severity"`
	}
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
			return nil, fmt.Errorf("invalid tool arguments: %w", err)
		}
	}
	switch call.Name {
	case "complyscan_list_actions":
		value, path, err := readActionsReport(target, arguments.Report)
		if err != nil {
			return nil, err
		}
		actions, err := filterDeveloperActions(value.DeveloperActions, arguments.Status)
		if err != nil {
			return nil, err
		}
		sortDeveloperActions(actions)
		return map[string]any{"report_path": path, "scan_id": value.Scan.ID, "actions": actions}, nil
	case "complyscan_get_action":
		value, path, err := readActionsReport(target, arguments.Report)
		if err != nil {
			return nil, err
		}
		action, ok := findDeveloperAction(value.DeveloperActions, arguments.ID)
		if !ok {
			return nil, fmt.Errorf("action %q was not found in %s", arguments.ID, path)
		}
		return action, nil
	case "complyscan_scan_changed_code":
		if strings.TrimSpace(arguments.ChangedSince) == "" {
			return nil, errors.New("changed_since is required")
		}
		return runAgentDeterministicScan(target, arguments.ChangedSince, arguments.Severity, build, diagnostics)
	case "complyscan_verify_action":
		value, _, err := readActionsReport(target, arguments.Report)
		if err != nil {
			return nil, err
		}
		action, ok := findDeveloperAction(value.DeveloperActions, arguments.ID)
		if !ok {
			return nil, fmt.Errorf("action %q was not found", arguments.ID)
		}
		if action.Category != "deterministic-finding" {
			return map[string]any{
				"action": action, "verified": false, "requires_explicit_workflow": true,
				"message": "This action depends on AI, execution, or human evidence. Use `complyscan actions verify` explicitly after reviewing provider and execution boundaries.",
			}, nil
		}
		fresh, err := runAgentDeterministicScan(target, "", "info", build, diagnostics)
		if err != nil {
			return nil, err
		}
		current, active := findDeveloperAction(fresh.DeveloperActions, action.ID)
		return map[string]any{"action_id": action.ID, "verified": !active, "current_action": current, "scan_id": fresh.ScanID}, nil
	default:
		return nil, fmt.Errorf("unknown tool %q", call.Name)
	}
}

type agentScanResult struct {
	ScanID           string                       `json:"scan_id"`
	Summary          report.Summary               `json:"summary"`
	Findings         []rules.Finding              `json:"findings"`
	DeveloperActions []report.DeveloperAction     `json:"developer_actions"`
	Warnings         []string                     `json:"warnings,omitempty"`
	Repository       *report.RepositoryProvenance `json:"repository,omitempty"`
}

func runAgentDeterministicScan(target, changedSince, severity string, build BuildInfo, diagnostics io.Writer) (agentScanResult, error) {
	var output bytes.Buffer
	scan := newScanCommand(&output, build)
	scan.SetOut(&output)
	scan.SetErr(diagnostics)
	for name, value := range map[string]string{"format": "json", "severity": defaultString(severity, "info")} {
		if err := scan.Flags().Set(name, value); err != nil {
			return agentScanResult{}, err
		}
	}
	if err := scan.Flags().Set("deterministic-only", "true"); err != nil {
		return agentScanResult{}, err
	}
	if err := scan.Flags().Set("no-report", "true"); err != nil {
		return agentScanResult{}, err
	}
	if changedSince != "" {
		if err := scan.Flags().Set("changed-since", changedSince); err != nil {
			return agentScanResult{}, err
		}
	}
	scanErr := scan.RunE(scan, []string{target})
	var status *exitError
	if scanErr != nil && (!errors.As(scanErr, &status) || status.code != 1) {
		return agentScanResult{}, scanErr
	}
	var value report.Report
	if err := json.Unmarshal(output.Bytes(), &value); err != nil {
		return agentScanResult{}, fmt.Errorf("decode deterministic scan: %w", err)
	}
	value = report.EnsureDeveloperActions(value)
	return agentScanResult{
		ScanID: value.Scan.ID, Summary: value.Summary, Findings: value.Findings,
		DeveloperActions: value.DeveloperActions, Warnings: value.Warnings, Repository: value.Scan.Repository,
	}, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func agentMCPToolResult(value any) map[string]any {
	encoded, _ := json.Marshal(value)
	return map[string]any{
		"content":           []map[string]string{{"type": "text", "text": string(encoded)}},
		"structuredContent": value,
		"isError":           false,
	}
}

func agentMCPToolError(err error) map[string]any {
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": err.Error()}},
		"isError": true,
	}
}
