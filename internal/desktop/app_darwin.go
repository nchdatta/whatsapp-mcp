package desktop

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// AppSupported reports whether this system can run Claude Desktop.
func AppSupported() bool { return true }

// AppRunning reports whether Claude Desktop is running.
func AppRunning() bool {
	return exec.Command("pgrep", "-x", "Claude").Run() == nil
}

// QuitApp asks Claude Desktop to quit and waits for it to exit.
func QuitApp() error {
	if out, err := exec.Command("osascript", "-e", `tell application "Claude" to quit`).CombinedOutput(); err != nil {
		return fmt.Errorf("closing Claude Desktop: %v: %s", err, strings.TrimSpace(string(out)))
	}
	for i := 0; i < 60 && AppRunning(); i++ {
		time.Sleep(250 * time.Millisecond)
	}
	if AppRunning() {
		return fmt.Errorf("Claude Desktop didn't quit; quit it from the menu bar")
	}
	return nil
}

// StartApp launches Claude Desktop.
func StartApp() error {
	if out, err := exec.Command("open", "-a", "Claude").CombinedOutput(); err != nil {
		return fmt.Errorf("starting Claude Desktop: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
