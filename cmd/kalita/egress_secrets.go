package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/avangerus/kalita/internal/secrets"
)

// loadEgressSecrets resolves the runtime-injected destination credentials for
// the webhook runner (English-first release, D-011 hardening).
// KALITA_EGRESS_SECRETS names a JSON file mapping normalized egress domain to
// Authorization header value, e.g. {"hooks.example.com":"Bearer sk_live_..."}.
// The file must be mode 0600 on POSIX (checked at boot, fail-fast). Secrets
// NEVER appear in the DSL, a prompt, the journal or git — only this runtime
// injection. Loaded ONCE at boot; changing the file requires a node restart
// (deliberate: runtime secrets are not hot-reloaded).
//
// Returns (map, configured). configured==false means the operator set no
// KALITA_EGRESS_SECRETS at all — the runner then sends UNAUTHENTICATED
// webhooks, preserving the behaviour of nodes that predate this hardening.
// configured==true with a destination that has no entry FAILS CLOSED
// ("auth_missing") rather than silently sending unauthenticated.
func loadEgressSecrets() (map[string]string, bool) {
	path := os.Getenv("KALITA_EGRESS_SECRETS")
	if path == "" {
		return nil, false
	}
	if err := secrets.CheckOwnerOnlyPerm(path); err != nil {
		log.Fatalf("egress secrets: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("egress secrets: KALITA_EGRESS_SECRETS=%s: %v", path, err)
	}
	out, err := parseEgressSecrets(raw)
	if err != nil {
		log.Fatalf("egress secrets: %s: %v", path, err)
	}
	log.Printf("egress secrets: loaded %d destination credential(s) from %s", len(out), path)
	return out, true
}

// parseEgressSecrets validates a KALITA_EGRESS_SECRETS file body: a JSON
// object of domain -> Authorization value. Keys are normalized the same way
// the egress allowlist is (case, trailing dot, default port), and a
// half-configured file (empty domain or empty credential) is refused — a
// silent missing credential would mean unauthenticated delivery on a node
// whose operator believes secrets are enforced.
func parseEgressSecrets(raw []byte) (map[string]string, error) {
	var file map[string]string
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, &egressSecretsError{"not a JSON object of {\"domain\": \"Authorization value\"}: " + err.Error()}
	}
	out := make(map[string]string, len(file))
	for domain, value := range file {
		if domain == "" || value == "" {
			return nil, &egressSecretsError{"contains an empty domain or an empty credential — refusing to run with a half-configured secrets file"}
		}
		norm := normalizeHost(domain)
		if norm == "" {
			return nil, &egressSecretsError{"entry " + domain + " is not a usable domain"}
		}
		if prev, ok := out[norm]; ok && prev != value {
			return nil, &egressSecretsError{"two entries normalize to the same domain " + norm + " with different credentials"}
		}
		out[norm] = value
	}
	return out, nil
}

// egressSecretsError is the typed failure of parseEgressSecrets — the loader
// turns it into a boot-time log.Fatal, tests assert on its message.
type egressSecretsError struct{ msg string }

func (e *egressSecretsError) Error() string { return e.msg }
