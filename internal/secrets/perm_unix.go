//go:build !windows

package secrets

import (
	"fmt"
	"os"
)

// checkOwnerOnlyPerm refuses any permission bits beyond owner read/write
// (0600) — group- or world-readable/writable secrets files fail closed
// (LLD-18 §1). POSIX permission bits map directly here.
func checkOwnerOnlyPerm(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("secrets: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("secrets: %s must be mode 0600 (owner-only), got %04o — "+
			"refusing to start with a group/world-readable secrets file (SECURITY.md P1 #5)",
			path, fi.Mode().Perm())
	}
	return nil
}
