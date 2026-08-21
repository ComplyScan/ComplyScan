package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func TestAnthropicWireSchemaCompactsProfileDraftObjectUnion(t *testing.T) {
	original := profileDraftSchema()
	wire := anthropicOutputSchema(original)
	if path := remoteSchemaKeywordPath(wire, "anyOf", "$"); path != "" {
		t.Fatalf("Anthropic profile-draft wire schema retained an expensive object union at %s", path)
	}
	suggestions := wire["properties"].(map[string]any)["suggestions"].(map[string]any)
	item := suggestions["items"].(map[string]any)
	properties := item["properties"].(map[string]any)
	fields := properties["field"].(map[string]any)["enum"].([]any)
	if len(fields) != len(profile.CodeFactFields()) {
		t.Fatalf("compacted field enum has %d values, want %d: %#v", len(fields), len(profile.CodeFactFields()), fields)
	}
	valueItems := properties["values"].(map[string]any)["items"].(map[string]any)
	if valueItems["type"] != "string" {
		t.Fatalf("compacted value item schema = %#v, want string", valueItems)
	}
	if path := remoteSchemaKeywordPath(original, "anyOf", "$"); path == "" {
		t.Fatal("Anthropic compaction mutated the original strict local schema")
	}
}

func TestAnthropicWireSchemaCompactsRepositoryBlockAndFactUnions(t *testing.T) {
	request := RepositoryAnalysisRequest{Mode: RepositoryAnalysisTargeted, CompactSource: true}
	for index := 0; index < 20; index++ {
		request.Files = append(request.Files, RepositorySourceFile{
			BlockID: fmt.Sprintf("block-%d", index), Path: fmt.Sprintf("src/file-%d.go", index),
			ContentStartLine: 1, Content: "package source\n",
		})
	}
	original := repositorySourceObservationSchema(request, false)
	wire := anthropicOutputSchema(original)
	if path := remoteSchemaKeywordPath(wire, "anyOf", "$"); path != "" {
		t.Fatalf("Anthropic repository wire schema retained an expensive object union at %s", path)
	}
	if path := remoteSchemaKeywordPath(original, "anyOf", "$"); path == "" {
		t.Fatal("Anthropic compaction mutated the original repository schema")
	}
}

func TestAnthropicWireSchemaRemovesUnsupportedConstraints(t *testing.T) {
	original := providerPortabilityTestSchema()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		outputConfig := body["output_config"].(map[string]any)
		format := outputConfig["format"].(map[string]any)
		schema := format["schema"].(map[string]any)

		for _, keyword := range []string{
			"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf",
			"minLength", "maxLength", "maxItems", "uniqueItems",
		} {
			if path := remoteSchemaKeywordPath(schema, keyword, "$"); path != "" {
				t.Fatalf("Anthropic wire schema contains unsupported %s at %s: %#v", keyword, path, schema)
			}
		}
		properties := schema["properties"].(map[string]any)
		list := properties["list"].(map[string]any)
		if list["minItems"] != float64(1) {
			t.Fatalf("Anthropic-supported minItems=1 was not preserved: %#v", list)
		}
		choice := properties["choice"].(map[string]any)
		variants := choice["anyOf"].([]any)
		if variants[0].(map[string]any)["enum"] == nil || variants[1].(map[string]any)["const"] != "fixed" {
			t.Fatalf("Anthropic union constraints were not preserved: %#v", choice)
		}
		if schema["additionalProperties"] != false {
			t.Fatalf("Anthropic object schema is not closed: %#v", schema)
		}
		return testJSONResponse(http.StatusOK, map[string]any{
			"stop_reason": "end_turn",
			"content":     []any{map[string]any{"type": "text", "text": `{}`}},
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		}), nil
	})}

	completion := anthropicCompletion(client, "test-key", "test-model")
	if _, err := completion(context.Background(), portabilityChatRequest(original)); err != nil {
		t.Fatal(err)
	}
	if _, ok := original["properties"].(map[string]any)["name"].(map[string]any)["maxLength"]; !ok {
		t.Fatal("Anthropic wire transformation mutated the original local-validation schema")
	}
}

func TestGeminiWireSchemaUsesDocumentedSubsetAndCurrentRequestShape(t *testing.T) {
	original := providerPortabilityTestSchema()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		responseFormat := body["response_format"].(map[string]any)
		if responseFormat["type"] != "text" || responseFormat["mime_type"] != "application/json" {
			t.Fatalf("Gemini request uses the wrong structured-output shape: %#v", responseFormat)
		}
		if _, legacy := body["response_mime_type"]; legacy {
			t.Fatalf("Gemini request contains removed response_mime_type: %#v", body)
		}
		schema := responseFormat["schema"].(map[string]any)
		for _, keyword := range []string{"const", "minLength", "maxLength", "multipleOf", "uniqueItems"} {
			if path := remoteSchemaKeywordPath(schema, keyword, "$"); path != "" {
				t.Fatalf("Gemini wire schema contains unsupported %s at %s: %#v", keyword, path, schema)
			}
		}
		properties := schema["properties"].(map[string]any)
		count := properties["count"].(map[string]any)
		if count["minimum"] != float64(0) || count["maximum"] != float64(9) {
			t.Fatalf("Gemini numeric constraints were not preserved: %#v", count)
		}
		list := properties["list"].(map[string]any)
		if list["minItems"] != float64(1) || list["maxItems"] != float64(4) {
			t.Fatalf("Gemini array constraints were not preserved: %#v", list)
		}
		choice := properties["choice"].(map[string]any)
		variants := choice["anyOf"].([]any)
		if got := variants[1].(map[string]any)["enum"]; !reflect.DeepEqual(got, []any{"fixed"}) {
			t.Fatalf("Gemini const was not translated to a one-value enum: %#v", choice)
		}
		if schema["additionalProperties"] != false {
			t.Fatalf("Gemini additionalProperties constraint was lost: %#v", schema)
		}
		return testJSONResponse(http.StatusOK, map[string]any{
			"status": "completed",
			"steps": []any{map[string]any{
				"type": "model_output", "content": []any{map[string]any{"type": "text", "text": `{}`}},
			}},
			"usage": map[string]any{"total_input_tokens": 1, "total_output_tokens": 1},
		}), nil
	})}

	completion := geminiCompletion(client, "test-key", "test-model")
	if _, err := completion(context.Background(), portabilityChatRequest(original)); err != nil {
		t.Fatal(err)
	}
	choice := original["properties"].(map[string]any)["choice"].(map[string]any)
	if _, ok := choice["anyOf"].([]any)[1].(map[string]any)["const"]; !ok {
		t.Fatal("Gemini wire transformation mutated the original local-validation schema")
	}
}

func providerPortabilityTestSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type": "string", "minLength": 1, "maxLength": 20,
			},
			"count": map[string]any{
				"type": "integer", "minimum": 0, "maximum": 9, "multipleOf": 1,
			},
			"list": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"},
				"minItems": 1, "maxItems": 4, "uniqueItems": true,
			},
			"choice": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string", "enum": []string{"a", "b"}},
					map[string]any{"type": "string", "const": "fixed"},
				},
			},
		},
		"required":             []string{"name", "count", "list", "choice"},
		"additionalProperties": false,
	}
}

func portabilityChatRequest(schema map[string]any) ollamaChatRequest {
	return ollamaChatRequest{
		Messages: []ollamaMessage{
			{Role: "system", Content: "system"},
			{Role: "user", Content: "user"},
		},
		Format: schema,
	}
}

func remoteSchemaKeywordPath(value any, keyword, path string) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := path + "." + key
			if key == keyword {
				return childPath
			}
			if found := remoteSchemaKeywordPath(child, keyword, childPath); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := remoteSchemaKeywordPath(child, keyword, path); found != "" {
				return found
			}
		}
	}
	return ""
}
