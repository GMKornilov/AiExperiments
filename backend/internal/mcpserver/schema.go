package mcpserver

func emptySchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false}
}
func grinderInputSchema() map[string]any { return inputSchema("brand") }
func brewerInputSchema() map[string]any  { return inputSchema("brand", "brewMethod") }
func inputSchema(names ...string) map[string]any {
	properties := map[string]any{}
	for _, name := range names {
		properties[name] = map[string]any{"type": "string", "minLength": 1, "maxLength": 100}
	}
	return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
}
func grindersSchema() map[string]any {
	return catalogueSchema("grinders", map[string]any{"id": map[string]any{"type": "integer"}, "brand": map[string]any{"type": "string", "minLength": 1}, "model": map[string]any{"type": "string", "minLength": 1}, "minGrindIndex": map[string]any{"type": "number"}, "maxGrindIndex": map[string]any{"type": "number"}, "clicksPerFullRange": map[string]any{"type": "number"}}, true)
}
func brewersSchema() map[string]any {
	return catalogueSchema("brewers", map[string]any{"id": map[string]any{"type": "integer"}, "brand": map[string]any{"type": "string", "minLength": 1}, "model": map[string]any{"type": "string", "minLength": 1}, "brewMethod": map[string]any{"type": "string", "minLength": 1}, "defaultWaterTempF": map[string]any{"type": "number"}}, true)
}
func filtersSchema() map[string]any {
	return catalogueSchema("filters", map[string]any{"id": map[string]any{"type": "integer"}, "name": map[string]any{"type": "string", "minLength": 1}, "type": map[string]any{"type": "string", "minLength": 1}, "description": map[string]any{"type": "string"}}, false)
}
func methodsSchema() map[string]any {
	return outputSchema(map[string]any{"type": "object", "required": []string{"methods", "count"}, "properties": map[string]any{"methods": map[string]any{"type": "array", "items": map[string]any{"type": "object", "minProperties": 1}}, "count": map[string]any{"type": "integer", "minimum": 0}}})
}
func catalogueSchema(key string, fields map[string]any, includeBrands bool) map[string]any {
	item := map[string]any{"type": "object", "properties": fields, "additionalProperties": false}
	required := make([]string, 0, len(fields))
	for name := range fields {
		required = append(required, name)
	}
	item["required"] = required
	properties := map[string]any{key: map[string]any{"type": "array", "items": item}, "count": map[string]any{"type": "integer", "minimum": 0}}
	required = []string{key, "count"}
	if includeBrands {
		properties["brands"] = map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1}}
		required = append(required, "brands")
	}
	return outputSchema(map[string]any{"type": "object", "required": required, "properties": properties, "additionalProperties": false})
}

func outputSchema(success map[string]any) map[string]any {
	return map[string]any{"type": "object", "oneOf": []any{success, errorSchema()}}
}

func errorSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"code", "message", "retryable"}, "properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}, "retryable": map[string]any{"type": "boolean"}, "retryAfter": map[string]any{"type": "string"}}, "additionalProperties": false}
}
