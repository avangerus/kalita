//go:build windows

package secrets

import (
	"fmt"
	"os"
)

// checkOwnerOnlyPerm on Windows: POSIX permission bits (0600 etc.) do not
// exist here — Go's os package synthesizes a mode from the read-only file
// attribute, not from an ACL, so group/world-readable cannot be distinguished
// the way perm_unix.go does. This is a known gap (kalita's production target
// is on-prem Linux/Docker per SECURITY.md rule 6; Windows is a dev platform
// only): we verify the file exists and is readable and stop there, rather
// than pretend to enforce an ACL check we cannot actually perform. Do not
// treat a green result here as equivalent to the POSIX guarantee.
func checkOwnerOnlyPerm(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("secrets: %w", err)
	}
	return nil
}
