package brewmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientGrindersAddsTokenAndNormalizesBrands(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		if got := request.URL.Query().Get("brand"); got != "Hario" {
			t.Errorf("brand = %q", got)
		}
		_, _ = w.Write([]byte(`{"grinders":[{"id":1,"brand":"Hario","model":"M","minGrindIndex":1,"maxGrindIndex":2,"clicksPerFullRange":3}],"brands":["Hario","Hario"]}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Token: " secret ", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Grinders(context.Background(), "Hario")
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 1 || len(result.Brands) != 1 || result.Brands[0] != "Hario" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestClientErrors(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/grinders":
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(http.StatusTooManyRequests)
		case "/api/filters":
			_, _ = w.Write([]byte(`{"filters":[{"id":1,"name":"f"}]}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Grinders(context.Background(), "")
	if got := err.(*ToolError).Code; got != UpstreamRateLimited {
		t.Fatalf("code = %s", got)
	}
	_, err = client.Filters(context.Background())
	if got := err.(*ToolError).Code; got != BadUpstreamResponse {
		t.Fatalf("code = %s", got)
	}
}

func TestClientMethodsAcceptsDataEnvelope(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"name":"V60","ratio":16}]}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Methods(context.Background())
	if err != nil || result.Count != 1 || result.Methods[0]["name"] != "V60" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestClientBrandsRequiresValidProvidedArray(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"grinders":[{"id":1,"brand":"Hario","model":"M"}],"brands":null}`,
		`{"grinders":[{"id":1,"brand":"Hario","model":"M"}],"brands":"Hario"}`,
		`{"grinders":[{"id":1,"brand":"Hario","model":"M"}],"brands":[""]}`,
	} {
		t.Run(body, func(t *testing.T) {
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

func TestClientBrandsFallsBackOnlyWhenAbsent(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"grinders":[{"id":1,"brand":"Hario","model":"M"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Grinders(context.Background(), "")
	if err != nil || len(result.Brands) != 1 || result.Brands[0] != "Hario" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
