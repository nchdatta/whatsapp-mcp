//go:build !darwin

package secret

import "path/filepath"

// keyFile is where the protected master key lives on Windows and Linux.
func keyFile(dataDir string) string { return filepath.Join(dataDir, "key") }
