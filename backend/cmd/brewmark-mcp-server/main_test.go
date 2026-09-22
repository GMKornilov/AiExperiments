package main

import "testing"

func TestRequestTimeout(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"0", "-1s", "invalid"} {
		if _, err := requestTimeout(value); err == nil { t.Fatalf("requestTimeout(%q) succeeded", value) }
	}
	if timeout, err := requestTimeout(""); err != nil || timeout.String() != "10s" { t.Fatalf("default = %v, %v", timeout, err) }
}
