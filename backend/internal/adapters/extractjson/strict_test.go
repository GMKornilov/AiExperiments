package extractjson

import "testing"

func TestFactsStrictShape(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"global_facts":[]}`, `{"project_facts":[]}`, `{"global_facts":null,"project_facts":[]}`, `{"global_facts":[],"project_facts":null}`, `{"global_facts":[],"project_facts":[],"other":[]}`, `{"global_facts":[1],"project_facts":[]}`, `{"global_facts":[" "],"project_facts":[]}`, `{"global_facts":["same","same"],"project_facts":[]}`, `{"global_facts":[],"global_facts":[],"project_facts":[]}`, `{"global_facts":[],"project_facts":[]} {}`, `{"global_facts":{},"project_facts":[]}`} {
		t.Run(raw, func(t *testing.T) {
			if _, err := DecodeFacts(raw); err == nil {
				t.Fatal("accepted invalid memory")
			}
		})
	}
	if v, e := DecodeFacts(`{"global_facts":[],"project_facts":[]}`); e != nil || v.GlobalFacts == nil || v.ProjectFacts == nil {
		t.Fatal("valid clear rejected")
	}
}
