package main

import (
	"regexp"
	"strings"
	"testing"
)

// D-022: `kalita upgrade` verifies the release image keyless, against the
// release workflow's identity on a version tag, unless a key is given.
func TestCosignVerifyArgs(t *testing.T) {
	keyless := cosignVerifyArgs("ghcr.io/avangerus/kalita:v1.0.0", "", releaseIdentity, releaseIssuer)
	want := []string{"verify", "--certificate-identity-regexp", releaseIdentity,
		"--certificate-oidc-issuer", releaseIssuer, "ghcr.io/avangerus/kalita:v1.0.0"}
	if strings.Join(keyless, " ") != strings.Join(want, " ") {
		t.Fatalf("keyless args: %v", keyless)
	}
	if got := cosignVerifyArgs("img:v1", "cosign.pub", releaseIdentity, releaseIssuer); strings.Join(got, " ") != "verify --key cosign.pub img:v1" {
		t.Fatalf("key args: %v", got)
	}
	re := regexp.MustCompile(releaseIdentity)
	for id, ok := range map[string]bool{
		"https://github.com/avangerus/kalita/.github/workflows/release.yml@refs/tags/v1.0.0":      true,
		"https://github.com/avangerus/kalita/.github/workflows/release.yml@refs/tags/v1.0.0-rc.7": true,
		"https://github.com/avangerus/kalita/.github/workflows/release.yml@refs/heads/main":       false,
		"https://github.com/avangerus/kalita/.github/workflows/ci.yml@refs/tags/v1.0.0":           false,
		"https://github.com/evil/kalita/.github/workflows/release.yml@refs/tags/v1.0.0":           false,
		"https://github.com/avangerus/kalita-fork/.github/workflows/release.yml@refs/tags/v1.0.0": false,
	} {
		if re.MatchString(id) != ok {
			t.Errorf("identity %s: match=%v, want %v", id, !ok, ok)
		}
	}
}

func TestReleaseIdentityDotsAreLiteral(t *testing.T) {
	if regexp.MustCompile(releaseIdentity).MatchString("https://githubXcom/avangerus/kalita/.github/workflows/release.yml@refs/tags/v1") {
		t.Fatal("dots in the identity regexp must be escaped")
	}
}
