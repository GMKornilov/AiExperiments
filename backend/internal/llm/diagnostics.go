package llm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strings"
)

type requestIDKey struct{}

// redactDiagnostic excludes the configured credential and endpoint from opt-in logs.
func (c *Client) redactDiagnostic(value string) string {
	for _, secret := range []string{c.apiKey, c.baseURL} {
		if secret == "" {
			continue
		}
		encoded, _ := json.Marshal(secret)
		value = strings.ReplaceAll(value, string(encoded[1:len(encoded)-1]), "[REDACTED]")
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}

// WithRequestID uses a bounded hexadecimal correlation ID or generates a new one.
func WithRequestID(ctx context.Context, candidate string) context.Context {
	valid := len(candidate) == 32 || len(candidate) == 36
	for _, char := range candidate {
		if !strings.ContainsRune("0123456789abcdef-", char) {
			valid = false
		}
	}
	if !valid {
		var value [16]byte
		_, _ = rand.Read(value[:])
		candidate = hex.EncodeToString(value[:])
	}
	return context.WithValue(ctx, requestIDKey{}, candidate)
}

// RequestID returns the server-side correlation identifier.
func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func transportFailure(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return "timeout"
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return "dns"
	}
	return "network"
}
