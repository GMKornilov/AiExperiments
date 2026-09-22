// Package mcpclient provides a bounded MCP tools registry client.
package mcpclient

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	requestTimeout  = 5 * time.Second
	maxResponseSize = 64 * 1024
)

// ErrorCode is a safe public error category for the MCP registry boundary.
type ErrorCode string

const (
	NotConfigured   ErrorCode = "MCP_NOT_CONFIGURED"
	Unavailable     ErrorCode = "MCP_UNAVAILABLE"
	Timeout         ErrorCode = "MCP_TIMEOUT"
	ProtocolError   ErrorCode = "MCP_PROTOCOL_ERROR"
	InvalidResponse ErrorCode = "MCP_INVALID_RESPONSE"
)

// Error is a safe error returned to the HTTP boundary.
type Error struct{ Code ErrorCode }

func (e *Error) Error() string { return string(e.Code) }

// Tool is the intentionally small public projection of an MCP tool.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Client establishes a short-lived Streamable HTTP connection for each call.
type Client struct {
	endpoint string
	http     *http.Client
}

// New validates server-owned configuration without exposing its value.
func New(endpoint string) (*Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, &Error{Code: NotConfigured}
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return nil, &Error{Code: NotConfigured}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &Client{
		endpoint: parsed.String(),
		http: &http.Client{
			Timeout:   requestTimeout,
			Transport: responseLimitTransport{next: transport},
		},
	}, nil
}

// ListTools initializes an MCP session, lists the registry and always closes it.
func (c *Client) ListTools(ctx context.Context, correlationID string) ([]Tool, error) {
	if c == nil {
		return nil, &Error{Code: NotConfigured}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "barista-api", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             c.endpoint,
		HTTPClient:           correlationClient(c.http, correlationID),
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, classifyConnectError(err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, classifyListError(err)
	}
	tools := make([]Tool, len(result.Tools))
	for index, tool := range result.Tools {
		if tool == nil || strings.TrimSpace(tool.Name) == "" || strings.TrimSpace(tool.Description) == "" {
			return nil, &Error{Code: InvalidResponse}
		}
		tools[index] = Tool{Name: tool.Name, Description: tool.Description}
	}
	return tools, nil
}

func classifyConnectError(err error) error {
	if isTimeout(err) {
		return &Error{Code: Timeout}
	}
	if errors.Is(err, errResponseTooLarge) {
		return &Error{Code: InvalidResponse}
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return &Error{Code: Unavailable}
	}
	return &Error{Code: ProtocolError}
}

func classifyListError(err error) error {
	if isTimeout(err) {
		return &Error{Code: Timeout}
	}
	if errors.Is(err, errResponseTooLarge) {
		return &Error{Code: InvalidResponse}
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return &Error{Code: Unavailable}
	}
	return &Error{Code: ProtocolError}
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

type correlationTransport struct {
	next          http.RoundTripper
	correlationID string
}

func correlationClient(client *http.Client, correlationID string) *http.Client {
	if correlationID == "" {
		return client
	}
	clone := *client
	clone.Transport = correlationTransport{next: client.Transport, correlationID: correlationID}
	return &clone
}

func (t correlationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("X-Request-ID", t.correlationID)
	return t.next.RoundTrip(request)
}

var errResponseTooLarge = errors.New("MCP response exceeds the configured limit")

type responseLimitTransport struct{ next http.RoundTripper }

func (t responseLimitTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if err != nil || response == nil {
		return response, err
	}
	if response.ContentLength > maxResponseSize {
		_ = response.Body.Close()
		return nil, errResponseTooLarge
	}
	response.Body = &limitedReadCloser{ReadCloser: response.Body, remaining: maxResponseSize}
	return response, nil
}

type limitedReadCloser struct {
	io.ReadCloser
	remaining int64
}

func (r *limitedReadCloser) Read(data []byte) (int, error) {
	if r.remaining == 0 {
		var probe [1]byte
		count, err := r.ReadCloser.Read(probe[:])
		if count > 0 {
			return 0, errResponseTooLarge
		}
		return 0, err
	}
	if int64(len(data)) > r.remaining {
		data = data[:r.remaining]
	}
	count, err := r.ReadCloser.Read(data)
	r.remaining -= int64(count)
	return count, err
}
