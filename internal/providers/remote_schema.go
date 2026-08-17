package providers

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
	return portableRemoteSchema(schema, remoteSchemaAnthropic)
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
