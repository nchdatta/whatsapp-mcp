//go:build !windows

package desktop

// AddToUserPath is a no-op outside Windows: the binary goes to ~/.local/bin,
// which most shells already have on PATH (install.sh prints a tip otherwise).
func AddToUserPath(dir string) (bool, error) { return false, nil }

// RemoveFromUserPath is a no-op outside Windows.
func RemoveFromUserPath(dir string) (bool, error) { return false, nil }
