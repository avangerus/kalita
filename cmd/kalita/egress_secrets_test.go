package main

import (
	"strings"
	"testing"
)

// D-011 hardening — the KALITA_EGRESS_SECRETS file contract: format, key
// normalization and the half-configured-file refusal, all fail-fast.

func TestParseEgressSecrets(t *testing.T) {
	got, err := parseEgressSecrets([]byte(`{
		"hooks.example.com": "Bearer sk_live_1",
		"Hooks.EXAMPLE.COM.:443": "Bearer sk_live_1"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["hooks.example.com"] != "Bearer sk_live_1" {
		t.Fatalf("keys must normalize to one domain entry: %v", got)
	}

	// a second entry for the same normalized domain with a DIFFERENT
	// credential is ambiguous and must be refused
	if _, err := parseEgressSecrets([]byte(`{"a.example.com":"x","A.example.com.":"y"}`)); err == nil {
		t.Fatal("conflicting credentials for one normalized domain must be refused")
	}

	bad := []string{
		`not json`,
		`[]`,
		`{"": "Bearer x"}`,
		`{"a.example.com": ""}`,
		`{"a.example.com": 42}`,
		`{"/not/a/host": "Bearer x"}`,
	}
	for _, body := range bad {
		if _, err := parseEgressSecrets([]byte(body)); err == nil {
			t.Fatalf("must refuse %q", body)
		}
	}

	// the error must be readable as a boot-time diagnostic
	_, err = parseEgressSecrets([]byte(`{"": "Bearer x"}`))
	if err == nil || !strings.Contains(err.Error(), "half-configured") {
		t.Fatalf("want the half-configured diagnostic, got %v", err)
	}
}
