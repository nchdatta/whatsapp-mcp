package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
)

// removeProgram deletes the installed binary and its folder. Windows can't
// delete a running .exe, so a detached cmd does it once this process exits.
func removeProgram(bin string) error {
	script := fmt.Sprintf(`ping -n 3 127.0.0.1 >nul & del /f /q "%s" & rmdir "%s"`, bin, filepath.Dir(bin))
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       `cmd.exe /c "` + script + `"`,
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x00000200, // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	}
	return cmd.Start()
}
