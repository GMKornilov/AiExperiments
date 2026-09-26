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
	ID                int      `json:"id"`
	Brand             string   `json:"brand"`
	Name              string   `json:"name"`
	MinSetting        float64  `json:"minSetting"`
	MaxSetting        float64  `json:"maxSetting"`
	SettingUnit       string   `json:"settingUnit"`
	EspressoAnchor    float64  `json:"espressoAnchor"`
	FilterAnchor      float64  `json:"filterAnchor"`
	CoarseAnchor      float64  `json:"coarseAnchor"`
	MokaAnchor        *float64 `json:"mokaAnchor"`
	FrenchPressAnchor *float64 `json:"frenchPressAnchor"`
	BurrType          *string  `json:"burrType"`
	CreatedAt         string   `json:"createdAt"`
}
type Brewer struct {
	ID            int     `json:"id"`
	Brand         string  `json:"brand"`
	Name          string  `json:"name"`
	BrewMethod    string  `json:"brewMethod"`
	MinBatchGrams float64 `json:"minBatchGrams"`
	MaxBatchGrams float64 `json:"maxBatchGrams"`
	CreatedAt     string  `json:"createdAt"`
}
type Filter struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	GrindAdjustment float64 `json:"grindAdjustment"`
	CreatedAt       string  `json:"createdAt"`
}
type BrewMethod struct {
	ID                  string  `json:"id"`
	Label               string  `json:"label"`
	DefaultRatio        float64 `json:"defaultRatio"`
	DefaultGrindSetting float64 `json:"defaultGrindSetting"`
	Description         string  `json:"description"`
}
type GrindersResult struct {
	Grinders    []Grinder `json:"grinders"`
	Brands      []string  `json:"brands"`
	Count       int       `json:"count"`
	MatchStatus string    `json:"matchStatus"`
}
type BrewersResult struct {
	Brewers     []Brewer `json:"brewers"`
	Brands      []string `json:"brands"`
	Count       int      `json:"count"`
	MatchStatus string   `json:"matchStatus"`
}
type FiltersResult struct {
	Filters []Filter `json:"filters"`
	Count   int      `json:"count"`
}
type MethodsResult struct {
	Methods []BrewMethod `json:"methods"`
	Count   int          `json:"count"`
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
	return c.GrindersByName(ctx, brand, "")
}

// GrindersByName applies an exact, case-insensitive name filter after the
// upstream brand query. BrewMark does not provide a name query parameter.
func (c *Client) GrindersByName(ctx context.Context, brand, name string) (GrindersResult, error) {
	name = strings.TrimSpace(name)
	var source struct {
		Grinders *json.RawMessage `json:"grinders"`
		Brands   sourceBrands     `json:"brands"`
	}
	if err := c.get(ctx, "/api/grinders", url.Values{"brand": optional(brand)}, &source); err != nil {
		return GrindersResult{}, err
	}
	var items []Grinder
	if !decodeValidatedArray(source.Grinders, &items, validGrinderJSON) || !validGrinders(items) {
		return GrindersResult{}, invalidResponse()
	}
	brandValues, ok := brands(source.Brands, grinderBrands(items))
	if !ok {
		return GrindersResult{}, invalidResponse()
	}
	if name != "" {
		items = filterGrindersByName(items, name)
		brandValues = uniqueBrands(grinderBrands(items))
	}
	return GrindersResult{Grinders: items, Brands: brandValues, Count: len(items), MatchStatus: matchStatus(name, len(items))}, nil
}
func (c *Client) Brewers(ctx context.Context, brand string) (BrewersResult, error) {
	return c.BrewersByName(ctx, brand, "")
}

// BrewersByName applies an exact, case-insensitive name filter after the
// upstream brand query. BrewMark does not provide a name query parameter.
func (c *Client) BrewersByName(ctx context.Context, brand, name string) (BrewersResult, error) {
	name = strings.TrimSpace(name)
	var source struct {
		Machines *json.RawMessage `json:"machines"`
		Brands   sourceBrands     `json:"brands"`
	}
	if err := c.get(ctx, "/api/machines", url.Values{"brand": optional(brand)}, &source); err != nil {
		return BrewersResult{}, err
	}
	var items []Brewer
	if !decodeValidatedArray(source.Machines, &items, validBrewerJSON) || !validBrewers(items) {
		return BrewersResult{}, invalidResponse()
	}
	brandValues, ok := brands(source.Brands, brewerBrands(items))
	if !ok {
		return BrewersResult{}, invalidResponse()
	}
	if name != "" {
		items = filterBrewersByName(items, name)
		brandValues = uniqueBrands(brewerBrands(items))
	}
	return BrewersResult{Brewers: items, Brands: brandValues, Count: len(items), MatchStatus: matchStatus(name, len(items))}, nil
}

func filterGrindersByName(items []Grinder, name string) []Grinder {
	filtered := make([]Grinder, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Name), name) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterBrewersByName(items []Brewer, name string) []Brewer {
	filtered := make([]Brewer, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Name), name) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func matchStatus(name string, count int) string {
	if name == "" {
		return "not_requested"
	}
	if count == 0 {
		return "empty"
	}
	if count == 1 {
		return "exact"
	}
	return "ambiguous"
}
func (c *Client) Filters(ctx context.Context) (FiltersResult, error) {
	var source struct {
		Filters *json.RawMessage `json:"filters"`
	}
	if err := c.get(ctx, "/api/filters", nil, &source); err != nil {
		return FiltersResult{}, err
	}
	var items []Filter
	if !decodeValidatedArray(source.Filters, &items, validFilterJSON) {
		return FiltersResult{}, invalidResponse()
	}
	if !validFilters(items) {
		return FiltersResult{}, invalidResponse()
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
	var values []BrewMethod
	if source.Data == nil || !decodeValidatedArray(source.Data, &values, validMethodJSON) || !validMethods(values) {
		return MethodsResult{}, invalidResponse()
	}
	return MethodsResult{Methods: values, Count: len(values)}, nil
}

func decodeValidatedArray[T any](raw *json.RawMessage, target *[]T, validate func(json.RawMessage) bool) bool {
	if raw == nil || string(*raw) == "null" || !noDuplicateKeys(*raw) {
		return false
	}
	var values []json.RawMessage
	if err := json.Unmarshal(*raw, &values); err != nil || values == nil {
		return false
	}
	for _, value := range values {
		if !validate(value) {
			return false
		}
	}
	return json.Unmarshal(*raw, target) == nil && *target != nil
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

func validGrinderJSON(raw json.RawMessage) bool {
	return validObject(raw, map[string]valueKind{
		"id":                integerValue,
		"brand":             stringValue,
		"name":              stringValue,
		"minSetting":        numberValue,
		"maxSetting":        numberValue,
		"settingUnit":       stringValue,
		"espressoAnchor":    numberValue,
		"filterAnchor":      numberValue,
		"coarseAnchor":      numberValue,
		"mokaAnchor":        nullableNumberValue,
		"frenchPressAnchor": nullableNumberValue,
		"burrType":          nullableStringValue,
		"createdAt":         stringValue,
	})
}

func validBrewerJSON(raw json.RawMessage) bool {
	return validObject(raw, map[string]valueKind{
		"id":            integerValue,
		"brand":         stringValue,
		"name":          stringValue,
		"brewMethod":    stringValue,
		"minBatchGrams": numberValue,
		"maxBatchGrams": numberValue,
		"createdAt":     stringValue,
	})
}

func validFilterJSON(raw json.RawMessage) bool {
	return validObject(raw, map[string]valueKind{
		"id":              integerValue,
		"name":            stringValue,
		"grindAdjustment": numberValue,
		"createdAt":       stringValue,
	})
}

func validMethodJSON(raw json.RawMessage) bool {
	return validObject(raw, map[string]valueKind{
		"id":                  stringValue,
		"label":               stringValue,
		"defaultRatio":        numberValue,
		"defaultGrindSetting": numberValue,
		"description":         stringValue,
	})
}

type valueKind int

const (
	stringValue valueKind = iota
	numberValue
	integerValue
	nullableStringValue
	nullableNumberValue
)

func validObject(raw json.RawMessage, required map[string]valueKind) bool {
	if !noDuplicateKeys(raw) {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	for name, kind := range required {
		value, ok := fields[name]
		if !ok || !validValue(value, kind) {
			return false
		}
	}
	return true
}

func validValue(raw json.RawMessage, kind valueKind) bool {
	if string(raw) == "null" {
		return kind == nullableStringValue || kind == nullableNumberValue
	}
	switch kind {
	case stringValue, nullableStringValue:
		var value string
		return json.Unmarshal(raw, &value) == nil
	case numberValue, nullableNumberValue:
		var value float64
		return json.Unmarshal(raw, &value) == nil
	case integerValue:
		var value int
		return json.Unmarshal(raw, &value) == nil
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
		if strings.TrimSpace(v.Brand) == "" || strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.SettingUnit) == "" || !validTimestamp(v.CreatedAt) {
			return false
		}
		if v.BurrType != nil && strings.TrimSpace(*v.BurrType) == "" {
			return false
		}
	}
	return true
}
func validBrewers(items []Brewer) bool {
	for _, v := range items {
		if strings.TrimSpace(v.Brand) == "" || strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.BrewMethod) == "" || !validTimestamp(v.CreatedAt) {
			return false
		}
	}
	return true
}
func validFilters(items []Filter) bool {
	for _, v := range items {
		if strings.TrimSpace(v.Name) == "" || !validTimestamp(v.CreatedAt) {
			return false
		}
	}
	return true
}
func validMethods(items []BrewMethod) bool {
	for _, v := range items {
		if strings.TrimSpace(v.ID) == "" || strings.TrimSpace(v.Label) == "" {
			return false
		}
	}
	return true
}
func validTimestamp(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
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
func uniqueBrands(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
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
