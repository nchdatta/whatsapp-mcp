//go:build !windows && !darwin

package desktop

import "errors"

var errNoApp = errors.New("Claude Desktop isn't available on this system")

// AppSupported reports whether this system can run Claude Desktop.
func AppSupported() bool { return false }

// AppRunning reports whether Claude Desktop is running.
func AppRunning() bool { return false }

// QuitApp is unsupported here.
func QuitApp() error { return errNoApp }

// StartApp is unsupported here.
func StartApp() error { return errNoApp }
