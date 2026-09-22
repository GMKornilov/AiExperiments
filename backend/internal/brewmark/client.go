// Package brewmark provides a safe client for BrewMark's public catalogue API.
package brewmark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

type ErrorCode string

const (
	InvalidArgument     ErrorCode = "INVALID_ARGUMENT"
	UpstreamUnavailable ErrorCode = "UPSTREAM_UNAVAILABLE"
	UpstreamTimeout     ErrorCode = "UPSTREAM_TIMEOUT"
	UpstreamRateLimited ErrorCode = "UPSTREAM_RATE_LIMITED"
	BadUpstreamResponse ErrorCode = "BAD_UPSTREAM_RESPONSE"
)

type ToolError struct {
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
	Retryable  bool      `json:"retryable"`
	RetryAfter string    `json:"retryAfter,omitempty"`
}

func (e *ToolError) Error() string { return string(e.Code) }

type Config struct {
	BaseURL  string
	Token    string
	Timeout  time.Duration
	Logger   *slog.Logger
	Observer func(context.Context, string, string, int, time.Duration)
}

type Client struct {
	baseURL  *url.URL
	token    string
	http     *http.Client
	logger   *slog.Logger
	observer func(context.Context, string, string, int, time.Duration)
}

type correlationIDKey struct{}

// WithCorrelationID attaches a safe correlation ID to an upstream operation.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

// CorrelationID returns the request correlation ID, if one was attached.
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}

func NewClient(cfg Config) (*Client, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://brewmark.io"
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid BrewMark base URL")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Client{baseURL: parsed, token: strings.TrimSpace(cfg.Token), http: &http.Client{Timeout: cfg.Timeout}, logger: cfg.Logger, observer: cfg.Observer}, nil
}

type Grinder struct {
	ID                 int     `json:"id"`
	Brand              string  `json:"brand"`
	Model              string  `json:"model"`
	MinGrindIndex      float64 `json:"minGrindIndex"`
	MaxGrindIndex      float64 `json:"maxGrindIndex"`
	ClicksPerFullRange float64 `json:"clicksPerFullRange"`
}
type Brewer struct {
	ID                int     `json:"id"`
	Brand             string  `json:"brand"`
	Model             string  `json:"model"`
	BrewMethod        string  `json:"brewMethod"`
	DefaultWaterTempF float64 `json:"defaultWaterTempF"`
}
type Filter struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}
type GrindersResult struct {
	Grinders []Grinder `json:"grinders"`
	Brands   []string  `json:"brands"`
	Count    int       `json:"count"`
}
type BrewersResult struct {
	Brewers []Brewer `json:"brewers"`
	Brands  []string `json:"brands"`
	Count   int      `json:"count"`
}
type FiltersResult struct {
	Filters []Filter `json:"filters"`
	Count   int      `json:"count"`
}
type MethodsResult struct {
	Methods []map[string]any `json:"methods"`
	Count   int              `json:"count"`
}

type sourceBrands struct {
	present bool
	value   json.RawMessage
}

func (s *sourceBrands) UnmarshalJSON(data []byte) error {
	s.present = true
	s.value = append(s.value[:0], data...)
	return nil
}

func (c *Client) Grinders(ctx context.Context, brand string) (GrindersResult, error) {
	var source struct {
		Grinders *json.RawMessage `json:"grinders"`
		Brands   sourceBrands     `json:"brands"`
	}
	if err := c.get(ctx, "/api/grinders", url.Values{"brand": optional(brand)}, &source); err != nil {
		return GrindersResult{}, err
	}
	var items []Grinder
	if !requiredArray(source.Grinders, &items) || !validGrinders(items) {
		return GrindersResult{}, invalidResponse()
	}
	brandValues, ok := brands(source.Brands, grinderBrands(items))
	if !ok {
		return GrindersResult{}, invalidResponse()
	}
	return GrindersResult{Grinders: items, Brands: brandValues, Count: len(items)}, nil
}
func (c *Client) Brewers(ctx context.Context, brand, method string) (BrewersResult, error) {
	var source struct {
		Machines *json.RawMessage `json:"machines"`
		Brands   sourceBrands     `json:"brands"`
	}
	if err := c.get(ctx, "/api/machines", url.Values{"brand": optional(brand), "brewMethod": optional(method)}, &source); err != nil {
		return BrewersResult{}, err
	}
	var items []Brewer
	if !requiredArray(source.Machines, &items) || !validBrewers(items) {
		return BrewersResult{}, invalidResponse()
	}
	brandValues, ok := brands(source.Brands, brewerBrands(items))
	if !ok {
		return BrewersResult{}, invalidResponse()
	}
	return BrewersResult{Brewers: items, Brands: brandValues, Count: len(items)}, nil
}
func (c *Client) Filters(ctx context.Context) (FiltersResult, error) {
	var source struct {
		Filters *json.RawMessage `json:"filters"`
	}
	if err := c.get(ctx, "/api/filters", nil, &source); err != nil {
		return FiltersResult{}, err
	}
	var items []Filter
	if !requiredArray(source.Filters, &items) {
		return FiltersResult{}, invalidResponse()
	}
	for _, item := range items {
		if item.ID == 0 || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Type) == "" {
			return FiltersResult{}, invalidResponse()
		}
	}
	return FiltersResult{Filters: items, Count: len(items)}, nil
}
func (c *Client) Methods(ctx context.Context) (MethodsResult, error) {
	var source struct {
		Data *json.RawMessage `json:"data"`
	}
	if err := c.get(ctx, "/api/brew-methods", nil, &source); err != nil {
		return MethodsResult{}, err
	}
	var values []map[string]any
	if source.Data == nil || !noDuplicateKeys(*source.Data) || !requiredArray(source.Data, &values) {
		return MethodsResult{}, invalidResponse()
	}
	for _, value := range values {
		if len(value) == 0 || !hasScalar(value) {
			return MethodsResult{}, invalidResponse()
		}
	}
	return MethodsResult{Methods: values, Count: len(values)}, nil
}

func requiredArray[T any](raw *json.RawMessage, target *[]T) bool {
	if raw == nil || string(*raw) == "null" || json.Unmarshal(*raw, target) != nil || *target == nil {
		return false
	}
	return true
}

func noDuplicateKeys(raw json.RawMessage) bool {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if !readJSONValue(decoder) {
		return false
	}
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

func readJSONValue(decoder *json.Decoder) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return true
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			name, err := decoder.Token()
			if err != nil {
				return false
			}
			key, ok := name.(string)
			if !ok || seen[key] {
				return false
			}
			seen[key] = true
			if !readJSONValue(decoder) {
				return false
			}
		}
		_, err := decoder.Token()
		return err == nil
	case '[':
		for decoder.More() {
			if !readJSONValue(decoder) {
				return false
			}
		}
		_, err := decoder.Token()
		return err == nil
	default:
		return false
	}
}

func optional(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
func (c *Client) get(ctx context.Context, path string, query url.Values, target any) error {
	started := time.Now()
	operation := "brewmark_get"
	result := "success"
	category := ""
	status := 0
	defer func() {
		id := CorrelationID(ctx)
		c.logger.Info("brewmark.mcp", "correlation_id", id, "operation", operation, "outcome", result, "error_category", category, "duration_ms", time.Since(started).Milliseconds())
		if c.observer != nil {
			c.observer(ctx, result, category, status, time.Since(started))
		}
	}()
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		result, category = "failure", string(UpstreamUnavailable)
		return unavailable()
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			result, category = "failure", string(UpstreamTimeout)
			return timeout()
		}
		result, category = "failure", string(UpstreamUnavailable)
		return unavailable()
	}
	defer response.Body.Close()
	status = response.StatusCode
	if response.StatusCode == http.StatusTooManyRequests {
		result, category = "failure", string(UpstreamRateLimited)
		return &ToolError{Code: UpstreamRateLimited, Message: "BrewMark API rate limit reached", Retryable: true, RetryAfter: validRetryAfter(response.Header.Get("Retry-After"))}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result, category = "failure", string(UpstreamUnavailable)
		return unavailable()
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		result, category = "failure", string(BadUpstreamResponse)
		return invalidResponse()
	}
	if err := json.Unmarshal(body, target); err != nil {
		result, category = "failure", string(BadUpstreamResponse)
		return invalidResponse()
	}
	return nil
}
func invalidResponse() error {
	return &ToolError{Code: BadUpstreamResponse, Message: "BrewMark API returned an invalid response", Retryable: false}
}
func unavailable() error {
	return &ToolError{Code: UpstreamUnavailable, Message: "BrewMark API is temporarily unavailable", Retryable: true}
}
func timeout() error {
	return &ToolError{Code: UpstreamTimeout, Message: "BrewMark API request timed out", Retryable: true}
}
func validRetryAfter(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if _, err := time.Parse(time.RFC1123, value); err == nil {
		return value
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return value
}
func validGrinders(items []Grinder) bool {
	for _, v := range items {
		if v.ID == 0 || strings.TrimSpace(v.Brand) == "" || strings.TrimSpace(v.Model) == "" {
			return false
		}
	}
	return true
}
func validBrewers(items []Brewer) bool {
	for _, v := range items {
		if v.ID == 0 || strings.TrimSpace(v.Brand) == "" || strings.TrimSpace(v.Model) == "" || strings.TrimSpace(v.BrewMethod) == "" {
			return false
		}
	}
	return true
}
func grinderBrands(items []Grinder) []string {
	out := make([]string, 0, len(items))
	for _, v := range items {
		out = append(out, v.Brand)
	}
	return out
}
func brewerBrands(items []Brewer) []string {
	out := make([]string, 0, len(items))
	for _, v := range items {
		out = append(out, v.Brand)
	}
	return out
}
func brands(source sourceBrands, fallback []string) ([]string, bool) {
	values := fallback
	if source.present {
		if string(source.value) == "null" || json.Unmarshal(source.value, &values) != nil || values == nil {
			return nil, false
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, false
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, true
}
func hasScalar(value map[string]any) bool {
	for _, item := range value {
		switch item.(type) {
		case string, float64:
			return true
		}
	}
	return false
}
