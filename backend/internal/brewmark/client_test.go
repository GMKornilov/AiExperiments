package brewmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const grinderFixture = `{"id":1,"brand":"Timemore","name":" Chestnut C3 ","minSetting":0,"maxSetting":24,"settingUnit":"CLICKS","espressoAnchor":8,"filterAnchor":16,"coarseAnchor":22,"mokaAnchor":null,"frenchPressAnchor":null,"burrType":null,"createdAt":"2026-09-24T00:00:00Z"}`
const brewerFixture = `{"id":1,"brand":"Moccamaster","name":" KBGV ","brewMethod":"BATCH_BREW","minBatchGrams":250,"maxBatchGrams":1250,"createdAt":"2026-09-24T00:00:00Z"}`
const filterFixture = `{"id":1,"name":"Paper #4","grindAdjustment":1,"createdAt":"2026-09-24T00:00:00Z"}`
const methodFixture = `{"id":"V60","label":"V60","defaultRatio":16,"defaultGrindSetting":20,"description":"Pour over"}`

func TestClientCatalogFixtures(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/grinders":
			if request.Header.Get("Authorization") != "Bearer secret" || request.URL.Query().Get("brand") != "Timemore" {
				t.Fatalf("unexpected grinder request: %s", request.URL.String())
			}
			_, _ = w.Write([]byte(`{"grinders":[` + grinderFixture + `],"brands":["Timemore","Timemore"],"byBrand":{"Timemore":[]}}`))
		case "/api/machines":
			if request.URL.Query().Get("brand") != "Moccamaster" || request.URL.RawQuery != "brand=Moccamaster" {
				t.Fatalf("unexpected brewer request: %s", request.URL.String())
			}
			_, _ = w.Write([]byte(`{"machines":[` + brewerFixture + `],"brands":["Moccamaster"],"byBrand":{"Moccamaster":[]}}`))
		case "/api/filters":
			_, _ = w.Write([]byte(`{"filters":[` + filterFixture + `]}`))
		case "/api/brew-methods":
			_, _ = w.Write([]byte(`{"data":[` + methodFixture + `]}`))
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Token: " secret ", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := client.Grinders(context.Background(), "Timemore"); err != nil || result.Count != 1 || result.Grinders[0].Name != " Chestnut C3 " || result.Grinders[0].MokaAnchor != nil || result.Grinders[0].BurrType != nil {
		t.Fatalf("grinders=%#v err=%v", result, err)
	}
	if result, err := client.Brewers(context.Background(), "Moccamaster"); err != nil || result.Count != 1 || result.Brewers[0].MinBatchGrams != 250 {
		t.Fatalf("brewers=%#v err=%v", result, err)
	}
	if result, err := client.Filters(context.Background()); err != nil || result.Count != 1 || result.Filters[0].GrindAdjustment != 1 {
		t.Fatalf("filters=%#v err=%v", result, err)
	}
	if result, err := client.Methods(context.Background()); err != nil || result.Count != 1 || result.Methods[0].ID != "V60" {
		t.Fatalf("methods=%#v err=%v", result, err)
	}
}

func TestClientNameLookupFiltersLocally(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("name") != "" || request.URL.Query().Get("model") != "" {
			t.Fatal("name and model must not be sent upstream")
		}
		switch request.URL.Path {
		case "/api/grinders":
			_, _ = w.Write([]byte(`{"grinders":[` + grinderFixture + `,` + `{"id":2,"brand":"Timemore","name":"Other","minSetting":0,"maxSetting":24,"settingUnit":"CLICKS","espressoAnchor":8,"filterAnchor":16,"coarseAnchor":22,"mokaAnchor":1,"frenchPressAnchor":2,"burrType":"CONICAL","createdAt":"2026-09-24T00:00:00Z"}` + `],"brands":["Timemore"]}`))
		case "/api/machines":
			_, _ = w.Write([]byte(`{"machines":[` + brewerFixture + `,` + `{"id":2,"brand":"Moccamaster","name":"Duplicate","brewMethod":"BATCH_BREW","minBatchGrams":1,"maxBatchGrams":2,"createdAt":"2026-09-24T00:00:00Z"},` + `{"id":3,"brand":"Moccamaster","name":" duplicate ","brewMethod":"BATCH_BREW","minBatchGrams":1,"maxBatchGrams":2,"createdAt":"2026-09-24T00:00:00Z"}` + `],"brands":["Moccamaster"]}`))
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.GrindersByName(context.Background(), "Timemore", "chestnut c3")
	if err != nil || result.Count != 1 || result.MatchStatus != "exact" || len(result.Brands) != 1 {
		t.Fatalf("grinders=%#v err=%v", result, err)
	}
	result, err = client.GrindersByName(context.Background(), "Timemore", "missing")
	if err != nil || result.Count != 0 || result.MatchStatus != "empty" || len(result.Brands) != 0 {
		t.Fatalf("empty grinders=%#v err=%v", result, err)
	}
	brewers, err := client.BrewersByName(context.Background(), "Moccamaster", "duplicate")
	if err != nil || brewers.Count != 2 || brewers.MatchStatus != "ambiguous" {
		t.Fatalf("brewers=%#v err=%v", brewers, err)
	}
}

func TestClientRejectsInvalidRequiredAndNullableFields(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"grinders":[{"id":1,"brand":"A","name":"B","minSetting":1,"maxSetting":2,"settingUnit":"CLICKS","espressoAnchor":1,"filterAnchor":2,"coarseAnchor":3,"mokaAnchor":"wrong","frenchPressAnchor":null,"burrType":null,"createdAt":"2026-09-24T00:00:00Z"}]}`,
		`{"grinders":[{"id":1,"brand":"A","name":"B","minSetting":1,"maxSetting":2,"settingUnit":"CLICKS","espressoAnchor":1,"filterAnchor":2,"coarseAnchor":3,"mokaAnchor":null,"frenchPressAnchor":null,"burrType":"","createdAt":"2026-09-24T00:00:00Z"}]}`,
		`{"grinders":[{"id":1,"brand":"A","name":"B","minSetting":1,"maxSetting":2,"settingUnit":"CLICKS","espressoAnchor":1,"filterAnchor":2,"coarseAnchor":3,"mokaAnchor":null,"frenchPressAnchor":null,"burrType":null}]}`,
	} {
		t.Run("invalid fixture", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			client, err := NewClient(Config{BaseURL: server.URL, Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Grinders(context.Background(), "")
			if toolErr, ok := err.(*ToolError); !ok || toolErr.Code != BadUpstreamResponse {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestClientErrors(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/grinders" {
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"filters":[{"id":1,"name":"f"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Grinders(context.Background(), ""); err.(*ToolError).Code != UpstreamRateLimited {
		t.Fatalf("err=%v", err)
	}
	if _, err = client.Filters(context.Background()); err.(*ToolError).Code != BadUpstreamResponse {
		t.Fatalf("err=%v", err)
	}
}
