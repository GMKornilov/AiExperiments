package brewmark

import "encoding/json"

// ToolContract is the shared, server-owned definition of a BrewMark tool.
// The MCP registry and the task LLM both project these exact definitions.
type ToolContract struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

const (
	listBrewMethodsDescription = "List BrewMark brew methods during research when the next execution needs a method baseline. Returns id, label, defaultRatio, defaultGrindSetting and description; defaults are starting points, not a generated recipe. It does not confirm user ownership."
	listBrewersDescription     = "Look up BrewMark brewers by optional brand and name during research when the next execution needs the brewer's method or supported batch range. Returns matching brand, name, brewMethod, minBatchGrams, maxBatchGrams and match status; it does not confirm user ownership or generate a recipe."
	listFiltersDescription     = "List BrewMark coffee filters during research when filter-specific grind compensation can affect the next execution. Returns name and grindAdjustment; the adjustment is a catalog starting point, not a generated recipe. It does not confirm user ownership."
	listGrindersDescription    = "Look up BrewMark grinders by optional brand and name during research when the next execution needs a model-specific starting grind setting. Returns matching brand, name, minSetting, maxSetting, settingUnit, espressoAnchor, filterAnchor, coarseAnchor, mokaAnchor, frenchPressAnchor, burrType and match status. An applicable anchor is a catalog starting point, not a generated recipe; the result does not confirm user ownership."
)

var toolContracts = []ToolContract{
	{Name: "brewmark_list_brew_methods", Description: listBrewMethodsDescription, InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`)},
	{Name: "brewmark_list_brewers", Description: listBrewersDescription, InputSchema: json.RawMessage(`{"type":"object","properties":{"brand":{"type":"string","minLength":1,"maxLength":100},"name":{"type":"string","minLength":1,"maxLength":100}},"additionalProperties":false}`)},
	{Name: "brewmark_list_filters", Description: listFiltersDescription, InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`)},
	{Name: "brewmark_list_grinders", Description: listGrindersDescription, InputSchema: json.RawMessage(`{"type":"object","properties":{"brand":{"type":"string","minLength":1,"maxLength":100},"name":{"type":"string","minLength":1,"maxLength":100}},"additionalProperties":false}`)},
}

// ToolContracts returns a defensive copy in the canonical lexicographic order.
func ToolContracts() []ToolContract {
	result := make([]ToolContract, len(toolContracts))
	for index, contract := range toolContracts {
		result[index] = ToolContract{Name: contract.Name, Description: contract.Description, InputSchema: append(json.RawMessage(nil), contract.InputSchema...)}
	}
	return result
}

func ToolContractByName(name string) (ToolContract, bool) {
	for _, contract := range toolContracts {
		if contract.Name == name {
			contract.InputSchema = append(json.RawMessage(nil), contract.InputSchema...)
			return contract, true
		}
	}
	return ToolContract{}, false
}
