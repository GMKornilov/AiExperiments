package brewmark

import (
	"strings"
	"testing"
)

func TestToolContractsDescribeRecipeRelevantCatalogValues(t *testing.T) {
	descriptions := make(map[string]string)
	for _, contract := range ToolContracts() {
		descriptions[contract.Name] = contract.Description
	}

	for name, fields := range map[string][]string{
		"brewmark_list_brew_methods": {"method baseline", "defaultRatio", "defaultGrindSetting"},
		"brewmark_list_brewers":      {"brewer's method", "minBatchGrams", "maxBatchGrams", "match status"},
		"brewmark_list_filters":      {"filter-specific grind compensation", "grindAdjustment"},
		"brewmark_list_grinders":     {"model-specific starting grind setting", "espressoAnchor", "filterAnchor", "frenchPressAnchor", "burrType", "match status"},
	} {
		description, ok := descriptions[name]
		if !ok {
			t.Fatalf("missing tool contract %q", name)
		}
		for _, field := range fields {
			if !strings.Contains(description, field) {
				t.Fatalf("description for %s misses %q: %q", name, field, description)
			}
		}
		if !strings.Contains(description, "does not confirm user ownership") {
			t.Fatalf("description for %s must preserve ownership boundary: %q", name, description)
		}
	}
}
