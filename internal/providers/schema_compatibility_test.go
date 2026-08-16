package providers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func TestStructuredSchemasUseSupportedFactUnions(t *testing.T) {
	tests := []struct {
		name     string
		schema   map[string]any
		variants func(*testing.T, map[string]any) []any
	}{
		{
			name:     "repository analysis",
			schema:   repositoryAnalysisSchema(RepositoryAnalysisFull, false, 0, 0),
			variants: repositoryFactSchemaVariants,
		},
		{
			name:     "profile draft",
			schema:   profileDraftSchema(),
			variants: profileDraftSchemaVariants,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if path := schemaKeywordPath(testCase.schema, "oneOf", "$"); path != "" {
				t.Fatalf("schema contains unsupported oneOf at %s", path)
			}
			assertFactSchemaVariants(t, testCase.variants(t, testCase.schema))
		})
	}
}

func TestReviewRepositoryRejectsValuesForTheWrongFactField(t *testing.T) {
	tests := []struct {
		name     string
		field    profile.CodeFactField
		value    string
		validFor profile.CodeFactField
	}{
		{
			name: "activity as lifecycle", field: profile.CodeFactLifecycleStage,
			value: "inference", validFor: profile.CodeFactAIActivities,
		},
		{
			name: "lifecycle as activity", field: profile.CodeFactAIActivities,
			value: "testing", validFor: profile.CodeFactLifecycleStage,
		},
		{
			name: "deployment as personal data", field: profile.CodeFactPersonalData,
			value: "api", validFor: profile.CodeFactDeploymentModels,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if !profile.CodeFactAllowsValue(testCase.validFor, testCase.value) {
				t.Fatalf("test value %q is no longer valid for %q", testCase.value, testCase.validFor)
			}
			if profile.CodeFactAllowsValue(testCase.field, testCase.value) {
				t.Fatalf("test value %q unexpectedly became valid for %q", testCase.value, testCase.field)
			}

			provider := &OllamaProvider{
				kind: OpenAI, label: "OpenAI", model: "test-model",
				completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
					content := fmt.Sprintf(`{"result":{"scope":".","ai_uses":[{"id":"known-use","name":"Known use","purpose":"Generate text","lifecycle":"runtime","confidence":"high","evidence":[{"path":"app.go","line":2,"summary":"Model call."}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"known-use","facts":[{"field":%q,"values":[%q],"confidence":"high","rationale":"The executable path supports this fact.","evidence":[{"path":"app.go","line":2,"summary":"Model call."}]}],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`, testCase.field, testCase.value)
					var response ollamaChatResponse
					response.Done = true
					response.Message.Content = content
					return response, nil
				},
			}
			_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
				Mode: RepositoryAnalysisFull, Scope: ".", RepositoryFiles: 1,
				Files: []RepositorySourceFile{{Path: "app.go", Kind: "source", Content: "package app\nfunc run() {}\n"}},
			})
			wantErr := fmt.Sprintf("unsupported %s value %q", testCase.field, testCase.value)
			if err == nil || !strings.Contains(err.Error(), wantErr) {
				t.Fatalf("expected trusted validation error %q, got %v", wantErr, err)
			}
		})
	}
}

func repositoryFactSchemaVariants(t *testing.T, schema map[string]any) []any {
	t.Helper()
	properties := schema["properties"].(map[string]any)
	result := properties["result"].(map[string]any)
	resultProperties := result["properties"].(map[string]any)
	factSets := resultProperties["ai_use_facts"].(map[string]any)
	factSetItems := factSets["items"].(map[string]any)
	factSetProperties := factSetItems["properties"].(map[string]any)
	facts := factSetProperties["facts"].(map[string]any)
	factItems := facts["items"].(map[string]any)
	variants, ok := factItems["anyOf"].([]any)
	if !ok {
		t.Fatalf("repository fact items do not contain an anyOf union: %#v", factItems)
	}
	return variants
}

func profileDraftSchemaVariants(t *testing.T, schema map[string]any) []any {
	t.Helper()
	properties := schema["properties"].(map[string]any)
	suggestions := properties["suggestions"].(map[string]any)
	items := suggestions["items"].(map[string]any)
	variants, ok := items["anyOf"].([]any)
	if !ok {
		t.Fatalf("profile-draft items do not contain an anyOf union: %#v", items)
	}
	return variants
}

func assertFactSchemaVariants(t *testing.T, variants []any) {
	t.Helper()
	wantFields := profile.CodeFactFields()
	if len(variants) != len(wantFields) {
		t.Fatalf("fact union has %d variants, want %d", len(variants), len(wantFields))
	}
	seen := make(map[profile.CodeFactField]struct{}, len(variants))
	for index, rawVariant := range variants {
		variant, ok := rawVariant.(map[string]any)
		if !ok {
			t.Fatalf("fact variant %d has type %T, want object schema", index, rawVariant)
		}
		properties, ok := variant["properties"].(map[string]any)
		if !ok {
			t.Fatalf("fact variant %d omitted properties", index)
		}
		if additional, ok := variant["additionalProperties"].(bool); !ok || additional {
			t.Fatalf("fact variant %d must reject additional properties", index)
		}
		required, ok := variant["required"].([]string)
		if !ok || !containsSchemaField(required, "field") {
			t.Fatalf("fact variant %d must require its discriminating field", index)
		}
		fieldSchema, ok := properties["field"].(map[string]any)
		if !ok {
			t.Fatalf("fact variant %d omitted field schema", index)
		}
		rawField, ok := fieldSchema["const"].(string)
		if !ok {
			t.Fatalf("fact variant %d omitted its constant field", index)
		}
		field, supported := profile.ParseCodeFactField(rawField)
		if !supported {
			t.Fatalf("fact variant %d uses unsupported field %q", index, rawField)
		}
		if _, duplicate := seen[field]; duplicate {
			t.Fatalf("fact union repeats field %q", field)
		}
		seen[field] = struct{}{}
	}
	for _, field := range wantFields {
		if _, ok := seen[field]; !ok {
			t.Errorf("fact union omitted field %q", field)
		}
	}
}

func containsSchemaField(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func schemaKeywordPath(value any, keyword, path string) string {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			childPath := path + "." + key
			if key == keyword {
				return childPath
			}
			if found := schemaKeywordPath(child, keyword, childPath); found != "" {
				return found
			}
		}
	case []any:
		for index, child := range value {
			childPath := fmt.Sprintf("%s[%d]", path, index)
			if found := schemaKeywordPath(child, keyword, childPath); found != "" {
				return found
			}
		}
	}
	return ""
}
