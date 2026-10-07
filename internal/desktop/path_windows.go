package desktop

import (
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// AddToUserPath appends dir to the current user's PATH (no admin rights
// needed). It reports whether PATH changed. New terminals pick it up; already
// open ones keep their old PATH.
func AddToUserPath(dir string) (bool, error) {
	return editUserPath(func(entries []string) ([]string, bool) {
		for _, e := range entries {
			if samePath(expand(e), dir) {
				return entries, false
			}
		}
		return append(entries, dir), true
	})
}

// RemoveFromUserPath drops dir from the current user's PATH.
func RemoveFromUserPath(dir string) (bool, error) {
	return editUserPath(func(entries []string) ([]string, bool) {
		kept := entries[:0]
		for _, e := range entries {
			if !samePath(expand(e), dir) {
				kept = append(kept, e)
			}
		}
		return kept, len(kept) != len(entries)
	})
}

func editUserPath(edit func([]string) ([]string, bool)) (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()

	current, valType, err := key.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, err
	}
	var entries []string
	for _, e := range strings.Split(current, ";") {
		if e = strings.TrimSpace(e); e != "" {
			entries = append(entries, e)
		}
	}
	updated, changed := edit(entries)
	if !changed {
		return false, nil
	}
	value := strings.Join(updated, ";")
	// Keep %VARIABLE% references working
	if valType == registry.SZ {
		err = key.SetStringValue("Path", value)
	} else {
		err = key.SetExpandStringValue("Path", value)
	}
	if err != nil {
		return false, err
	}
	broadcastEnvironmentChange()
	return true, nil
}

func expand(p string) string {
	if out, err := registry.ExpandString(p); err == nil {
		p = out
	}
	return filepath.Clean(p)
}

var procSendMessageTimeout = syscall.NewLazyDLL("user32.dll").NewProc("SendMessageTimeoutW")

// broadcastEnvironmentChange tells Explorer to reload environment variables,
// so terminals opened from now on see the new PATH without signing out.
func broadcastEnvironmentChange() {
	const (
		hwndBroadcast   = 0xFFFF
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	env, _ := syscall.UTF16PtrFromString("Environment")
	procSendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, 0)
}
