package providers

import "reflect"

// Hosted providers accept different subsets of JSON Schema for constrained
// output. Keep the complete schema on the request for local validation, and
// derive a provider-specific copy for the wire request. These transformations
// must never mutate the caller's schema.

type remoteSchemaDialect uint8

const (
	remoteSchemaAnthropic remoteSchemaDialect = iota + 1
	remoteSchemaGemini
)

var anthropicSchemaKeywords = map[string]struct{}{
	"$defs": {}, "$ref": {}, "definitions": {},
	"type": {}, "title": {}, "description": {}, "enum": {}, "const": {}, "default": {},
	"properties": {}, "required": {}, "additionalProperties": {},
	"items": {}, "minItems": {}, "anyOf": {}, "allOf": {},
	"format": {},
}

var geminiSchemaKeywords = map[string]struct{}{
	"$id": {}, "$defs": {}, "$ref": {}, "$anchor": {},
	"type": {}, "title": {}, "description": {}, "enum": {}, "format": {},
	"properties": {}, "required": {}, "additionalProperties": {},
	"items": {}, "prefixItems": {}, "minItems": {}, "maxItems": {},
	"minimum": {}, "maximum": {}, "anyOf": {}, "oneOf": {}, "propertyOrdering": {},
}

func anthropicOutputSchema(schema map[string]any) map[string]any {
	return compactAnthropicSchema(portableRemoteSchema(schema, remoteSchemaAnthropic))
}

func geminiOutputSchema(schema map[string]any) map[string]any {
	return portableRemoteSchema(schema, remoteSchemaGemini)
}

func portableRemoteSchema(schema map[string]any, dialect remoteSchemaDialect) map[string]any {
	allowed := geminiSchemaKeywords
	if dialect == remoteSchemaAnthropic {
		allowed = anthropicSchemaKeywords
	}

	result := make(map[string]any, len(schema))
	for keyword, value := range schema {
		// Gemini supports enum but not const. A one-value enum has the same
		// constrained-decoding effect; the original const remains available to
		// ComplyScan's local validator.
		if dialect == remoteSchemaGemini && keyword == "const" {
			if _, alreadyConstrained := schema["enum"]; !alreadyConstrained {
				result["enum"] = []any{cloneRemoteSchemaValue(value)}
			}
			continue
		}
		if _, ok := allowed[keyword]; !ok {
			continue
		}

		switch keyword {
		case "properties", "$defs", "definitions":
			result[keyword] = portableNamedRemoteSchemas(value, dialect)
		case "items", "additionalProperties":
			result[keyword] = portableRemoteSubschema(value, dialect)
		case "anyOf", "allOf", "oneOf", "prefixItems":
			result[keyword] = portableRemoteSchemaList(value, dialect)
		case "minItems":
			if dialect != remoteSchemaAnthropic || remoteSchemaZeroOrOne(value) {
				result[keyword] = cloneRemoteSchemaValue(value)
			}
		case "format":
			if remoteSchemaFormatSupported(value, dialect) {
				result[keyword] = cloneRemoteSchemaValue(value)
			}
		default:
			result[keyword] = cloneRemoteSchemaValue(value)
		}
	}

	// Anthropic requires closed object schemas. ComplyScan already generates
	// them this way, but normalizing here also protects future schemas.
	if dialect == remoteSchemaAnthropic && remoteSchemaIsObject(schema) {
		result["additionalProperties"] = false
	}
	return result
}

func portableNamedRemoteSchemas(value any, dialect remoteSchemaDialect) map[string]any {
	properties, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	result := make(map[string]any, len(properties))
	for name, rawSchema := range properties {
		result[name] = portableRemoteSubschema(rawSchema, dialect)
	}
	return result
}

func portableRemoteSubschema(value any, dialect remoteSchemaDialect) any {
	if schema, ok := value.(map[string]any); ok {
		return portableRemoteSchema(schema, dialect)
	}
	return cloneRemoteSchemaValue(value)
}

func portableRemoteSchemaList(value any, dialect remoteSchemaDialect) []any {
	values, ok := value.([]any)
	if !ok {
		return []any{}
	}
	result := make([]any, 0, len(values))
	for _, item := range values {
		result = append(result, portableRemoteSubschema(item, dialect))
	}
	return result
}

func cloneRemoteSchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = cloneRemoteSchemaValue(child)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = cloneRemoteSchemaValue(child)
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func remoteSchemaZeroOrOne(value any) bool {
	switch typed := value.(type) {
	case int:
		return typed == 0 || typed == 1
	case int64:
		return typed == 0 || typed == 1
	case float64:
		return typed == 0 || typed == 1
	default:
		return false
	}
}

func remoteSchemaIsObject(schema map[string]any) bool {
	if schema["type"] == "object" {
		return true
	}
	_, hasProperties := schema["properties"]
	return hasProperties
}

func remoteSchemaFormatSupported(value any, dialect remoteSchemaDialect) bool {
	format, ok := value.(string)
	if !ok {
		return false
	}
	if dialect == remoteSchemaGemini {
		switch format {
		case "date-time", "date", "time":
			return true
		default:
			return false
		}
	}
	switch format {
	case "date-time", "time", "date", "duration", "email", "hostname", "uri", "ipv4", "ipv6", "uuid":
		return true
	default:
		return false
	}
}

// compactAnthropicSchema collapses repeated disjoint object unions in the
// provider wire copy. Anthropic compiles structured-output schemas into a
// grammar and rejects large combinations of nested anyOf variants. ComplyScan
// still retains and locally validates the complete original schema, including
// field/value and block/line correlations.
func compactAnthropicSchema(schema map[string]any) map[string]any {
	result := cloneRemoteSchemaValue(schema).(map[string]any)
	for _, keyword := range []string{"properties", "$defs", "definitions"} {
		values, ok := result[keyword].(map[string]any)
		if !ok {
			continue
		}
		for name, value := range values {
			if child, ok := value.(map[string]any); ok {
				values[name] = compactAnthropicSchema(child)
			}
		}
	}
	for _, keyword := range []string{"items", "additionalProperties"} {
		if child, ok := result[keyword].(map[string]any); ok {
			result[keyword] = compactAnthropicSchema(child)
		}
	}
	if alternatives, ok := result["anyOf"].([]any); ok {
		compacted := make([]any, 0, len(alternatives))
		for _, value := range alternatives {
			if child, ok := value.(map[string]any); ok {
				compacted = append(compacted, compactAnthropicSchema(child))
			} else {
				compacted = append(compacted, cloneRemoteSchemaValue(value))
			}
		}
		if merged, ok := mergeAnthropicObjectAlternatives(compacted); ok {
			return merged
		}
		result["anyOf"] = compacted
	}
	return result
}

func mergeAnthropicObjectAlternatives(values []any) (map[string]any, bool) {
	if len(values) == 0 {
		return nil, false
	}
	objects := make([]map[string]any, 0, len(values))
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok || object["type"] != "object" {
			return nil, false
		}
		objects = append(objects, object)
	}
	firstProperties, ok := objects[0]["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	for _, object := range objects[1:] {
		properties, ok := object["properties"].(map[string]any)
		if !ok || !sameRemoteSchemaKeys(firstProperties, properties) || !reflect.DeepEqual(objects[0]["required"], object["required"]) {
			return nil, false
		}
	}
	mergedProperties := make(map[string]any, len(firstProperties))
	for name := range firstProperties {
		variants := make([]map[string]any, 0, len(objects))
		for _, object := range objects {
			properties := object["properties"].(map[string]any)
			variant, ok := properties[name].(map[string]any)
			if !ok {
				return nil, false
			}
			variants = append(variants, variant)
		}
		merged, ok := mergeAnthropicSchemaVariants(variants)
		if !ok {
			return nil, false
		}
		mergedProperties[name] = merged
	}
	return map[string]any{
		"type":                 "object",
		"properties":           mergedProperties,
		"required":             cloneRemoteSchemaValue(objects[0]["required"]),
		"additionalProperties": false,
	}, true
}

func mergeAnthropicSchemaVariants(values []map[string]any) (map[string]any, bool) {
	if len(values) == 0 {
		return nil, false
	}
	allEqual := true
	for _, value := range values[1:] {
		if !reflect.DeepEqual(values[0], value) {
			allEqual = false
			break
		}
	}
	if allEqual {
		return cloneRemoteSchemaValue(values[0]).(map[string]any), true
	}

	typeName, _ := values[0]["type"].(string)
	for _, value := range values[1:] {
		if value["type"] != typeName {
			return nil, false
		}
	}
	switch typeName {
	case "object":
		alternatives := make([]any, len(values))
		for index := range values {
			alternatives[index] = values[index]
		}
		return mergeAnthropicObjectAlternatives(alternatives)
	case "array":
		items := make([]map[string]any, 0, len(values))
		for _, value := range values {
			item, ok := value["items"].(map[string]any)
			if !ok {
				return nil, false
			}
			items = append(items, item)
		}
		mergedItems, ok := mergeAnthropicSchemaVariants(items)
		if !ok {
			return nil, false
		}
		result := map[string]any{"type": "array", "items": mergedItems}
		if minimum := commonAnthropicSchemaValue(values, "minItems"); minimum != nil {
			result["minItems"] = minimum
		}
		return result, true
	case "string":
		result := map[string]any{"type": "string"}
		if enum, constrained := mergedAnthropicStringEnum(values); constrained {
			result["enum"] = enum
		}
		return result, true
	case "integer", "number", "boolean":
		return map[string]any{"type": typeName}, true
	default:
		return nil, false
	}
}

func mergedAnthropicStringEnum(values []map[string]any) ([]any, bool) {
	result := make([]any, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		candidates := []any(nil)
		if constant, ok := value["const"]; ok {
			candidates = []any{constant}
		} else if enum, ok := value["enum"].([]any); ok {
			candidates = enum
		} else if enum, ok := value["enum"].([]string); ok {
			for _, candidate := range enum {
				candidates = append(candidates, candidate)
			}
		} else {
			return nil, false
		}
		for _, candidate := range candidates {
			text, ok := candidate.(string)
			if !ok {
				return nil, false
			}
			if _, duplicate := seen[text]; duplicate {
				continue
			}
			seen[text] = struct{}{}
			result = append(result, text)
		}
	}
	return result, true
}

func commonAnthropicSchemaValue(values []map[string]any, keyword string) any {
	first, exists := values[0][keyword]
	if !exists {
		return nil
	}
	for _, value := range values[1:] {
		if !reflect.DeepEqual(first, value[keyword]) {
			return nil
		}
	}
	return cloneRemoteSchemaValue(first)
}

func sameRemoteSchemaKeys(first, second map[string]any) bool {
	if len(first) != len(second) {
		return false
	}
	for key := range first {
		if _, exists := second[key]; !exists {
			return false
		}
	}
	return true
}
