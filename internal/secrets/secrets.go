// Package secrets resolves node secrets (Postgres DSN, TLS material, worker
// tokens) in the fallback order fixed by docs/lld/LLD-18-secrets-key-rotation.md
// §1: (1) a secrets config file, which MUST be exactly 0600 — refuses to start
// otherwise (fail-fast, SECURITY.md P1 #5); (2) a docker/systemd secret file
// named by the <NAME>_FILE convention; (3) the bare environment variable
// itself, logged as a warning ("not for prod"). A secret value is never
// logged or otherwise surfaced by this package — only ITS ABSENCE/fallback
// tier is reported.
package secrets

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Loader resolves secret values against an optionally-loaded secrets file.
// The zero value (or a Loader from NewLoader("")) has no file backing and
// resolves purely through the <NAME>_FILE / <NAME> env fallbacks.
type Loader struct {
	values map[string]string
}

// NewLoader reads the secrets file at path, if path is non-empty. The file
// must carry mode 0600 (owner read/write only) — group- or world-readable
// permissions refuse to load at all (LLD-18 §1: "отказ старта при
// group/world-readable"), so a misconfigured deploy fails loudly at boot
// rather than leaking a DSN to every local user. Format: one KEY=VALUE per
// line; blank lines and lines starting with # are ignored (matches the
// project's existing .env convention, see .env.example).
func NewLoader(path string) (*Loader, error) {
	if path == "" {
		return &Loader{}, nil
	}
	if err := checkOwnerOnlyPerm(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: %w", err)
	}
	defer f.Close()
	values := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("secrets: reading %s: %w", path, err)
	}
	return &Loader{values: values}, nil
}

// Resolve returns the value for name in the fallback order documented on
// Loader/NewLoader. warning is non-empty exactly when the value came from a
// bare environment variable (tier 3) — callers should log it once at boot.
// err is only ever a hard failure: an explicit <NAME>_FILE was set but could
// not be read (a misconfigured docker/systemd secret mount should fail loud,
// not silently fall through to a weaker tier).
func (l *Loader) Resolve(name string) (value, warning string, err error) {
	if l != nil && l.values != nil {
		if v, ok := l.values[name]; ok {
			return v, "", nil
		}
	}
	if fp := os.Getenv(name + "_FILE"); fp != "" {
		raw, ferr := os.ReadFile(fp)
		if ferr != nil {
			return "", "", fmt.Errorf("secrets: %s_FILE=%s: %w", name, fp, ferr)
		}
		return strings.TrimSpace(string(raw)), "", nil
	}
	if v := os.Getenv(name); v != "" {
		return v, fmt.Sprintf("%s is set from a plain environment variable — not for production; "+
			"use a 0600 secrets file (KALITA_SECRETS_FILE) or %s_FILE (docker/systemd secret)", name, name), nil
	}
	return "", "", nil
}
