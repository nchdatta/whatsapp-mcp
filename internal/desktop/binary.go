package desktop

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// BinaryPath is where install puts the executable, so the Claude Desktop
// config keeps working after the download is moved or deleted.
func BinaryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(local, "Programs", "whatsapp-mcp", "whatsapp-mcp.exe"), nil
	}
	return filepath.Join(home, ".local", "bin", "whatsapp-mcp"), nil
}

// ErrInUse means the installed binary is running (normally inside Claude Desktop).
var ErrInUse = errors.New("the installed whatsapp-mcp is in use; quit Claude Desktop and try again")

// Place copies the running executable to dst (unless it already is dst) and
// returns the path to register.
func Place(dst string) (string, error) {
	src, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(src); err == nil {
		src = resolved
	}
	if samePath(src, dst) {
		return dst, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}

	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()

	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	os.Remove(dst + ".old") // left by an earlier update, once nothing runs it
	if err := os.Rename(tmp, dst); err != nil {
		// Windows refuses to replace a running executable, but lets it be
		// renamed: move it aside so Claude Desktop picks up the new one on restart
		if runtime.GOOS == "windows" {
			if os.Rename(dst, dst+".old") == nil {
				if os.Rename(tmp, dst) == nil {
					return dst, nil
				}
				os.Rename(dst+".old", dst)
			}
			os.Remove(tmp)
			return "", fmt.Errorf("%w (%v)", ErrInUse, err)
		}
		os.Remove(tmp)
		return "", err
	}
	return dst, nil
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
