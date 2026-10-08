package desktop

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Claude Desktop's processes, told apart from Claude Code's claude.exe by
// path: the regular installer uses AnthropicClaude, the Store build WindowsApps.
const desktopProcs = `Get-Process -Name claude -ErrorAction SilentlyContinue | Where-Object { $_.Path -match '\\AnthropicClaude\\|\\WindowsApps\\Claude_' }`

// AppSupported reports whether this system can run Claude Desktop.
func AppSupported() bool { return true }

// AppRunning reports whether Claude Desktop is running.
func AppRunning() bool {
	out, err := powershell(`@(` + desktopProcs + `).Count`)
	return err == nil && strings.TrimSpace(out) != "0"
}

// QuitApp closes Claude Desktop, including its tray icon, and waits for it to exit.
func QuitApp() error {
	if _, err := powershell(desktopProcs + ` | Stop-Process -Force`); err != nil {
		return fmt.Errorf("closing Claude Desktop: %w", err)
	}
	for i := 0; i < 40 && AppRunning(); i++ {
		time.Sleep(250 * time.Millisecond)
	}
	if AppRunning() {
		return fmt.Errorf("couldn't close Claude Desktop; quit it from the system tray")
	}
	return nil
}

// StartApp launches Claude Desktop through its Start menu entry, which works
// for both the regular and the Store install.
func StartApp() error {
	_, err := powershell(`$a = Get-StartApps | Where-Object { $_.Name -eq 'Claude' } | Select-Object -First 1
if ($a) { Start-Process ("shell:AppsFolder\" + $a.AppID) }
elseif (Test-Path "$env:LOCALAPPDATA\AnthropicClaude\claude.exe") { Start-Process "$env:LOCALAPPDATA\AnthropicClaude\claude.exe" }
else { throw "Claude Desktop isn't installed" }`)
	if err != nil {
		return fmt.Errorf("starting Claude Desktop: %w", err)
	}
	return nil
}

func powershell(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
