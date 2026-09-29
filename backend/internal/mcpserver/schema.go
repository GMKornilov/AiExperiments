package mcpserver

func emptySchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false}
}
func grinderInputSchema() map[string]any { return inputSchema("brand", "name") }
func brewerInputSchema() map[string]any  { return inputSchema("brand", "name") }
func inputSchema(names ...string) map[string]any {
	properties := map[string]any{}
	for _, name := range names {
		properties[name] = map[string]any{"type": "string", "minLength": 1, "maxLength": 100}
	}
	return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
}
func grindersSchema() map[string]any {
	return catalogueSchema("grinders", map[string]any{"id": integerSchema(), "brand": nonEmptyStringSchema(), "name": nonEmptyStringSchema(), "minSetting": numberSchema(), "maxSetting": numberSchema(), "settingUnit": nonEmptyStringSchema(), "espressoAnchor": numberSchema(), "filterAnchor": numberSchema(), "coarseAnchor": numberSchema(), "mokaAnchor": nullableNumberSchema(), "frenchPressAnchor": nullableNumberSchema(), "burrType": nullableNonEmptyStringSchema(), "createdAt": map[string]any{"type": "string", "format": "date-time"}}, true)
}
func brewersSchema() map[string]any {
	return catalogueSchema("brewers", map[string]any{"id": integerSchema(), "brand": nonEmptyStringSchema(), "name": nonEmptyStringSchema(), "brewMethod": nonEmptyStringSchema(), "minBatchGrams": numberSchema(), "maxBatchGrams": numberSchema(), "createdAt": map[string]any{"type": "string", "format": "date-time"}}, true)
}
func filtersSchema() map[string]any {
	return catalogueSchema("filters", map[string]any{"id": integerSchema(), "name": nonEmptyStringSchema(), "grindAdjustment": numberSchema(), "createdAt": map[string]any{"type": "string", "format": "date-time"}}, false)
}
func methodsSchema() map[string]any {
	method := map[string]any{"type": "object", "required": []string{"id", "label", "defaultRatio", "defaultGrindSetting", "description"}, "properties": map[string]any{"id": nonEmptyStringSchema(), "label": nonEmptyStringSchema(), "defaultRatio": numberSchema(), "defaultGrindSetting": numberSchema(), "description": map[string]any{"type": "string"}}, "additionalProperties": false}
	return outputSchema(map[string]any{"type": "object", "required": []string{"methods", "count"}, "properties": map[string]any{"methods": map[string]any{"type": "array", "items": method}, "count": map[string]any{"type": "integer", "minimum": 0}}, "additionalProperties": false})
}

func integerSchema() map[string]any { return map[string]any{"type": "integer"} }
func numberSchema() map[string]any  { return map[string]any{"type": "number"} }
func nonEmptyStringSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1}
}
func nullableNumberSchema() map[string]any {
	return map[string]any{"type": []string{"number", "null"}}
}
func nullableNonEmptyStringSchema() map[string]any {
	return map[string]any{"type": []string{"string", "null"}, "minLength": 1}
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
		properties["matchStatus"] = map[string]any{"type": "string", "enum": []string{"not_requested", "exact", "empty", "ambiguous"}}
		required = append(required, "brands")
		required = append(required, "matchStatus")
	}
	return outputSchema(map[string]any{"type": "object", "required": required, "properties": properties, "additionalProperties": false})
}

func outputSchema(success map[string]any) map[string]any {
	return map[string]any{"type": "object", "oneOf": []any{success, errorSchema()}}
}

func errorSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"code", "message", "retryable"}, "properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}, "retryable": map[string]any{"type": "boolean"}, "retryAfter": map[string]any{"type": "string"}}, "additionalProperties": false}
}
