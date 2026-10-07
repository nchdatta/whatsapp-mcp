// Package config resolves where whatsapp-mcp keeps its data and sets up logging.
package config

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
)

// EnvDataDir overrides the data directory when --data is not given.
const EnvDataDir = "WHATSAPP_MCP_DATA"

// DefaultDataDir is the per-user location used when nothing else is configured.
func DefaultDataDir() string {
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "whatsapp-mcp")
	}
	return "whatsapp-mcp-data"
}

// DataDir picks the data directory: explicit flag, then env var, then the default.
// The directory is created if needed and returned as an absolute path.
func DataDir(flag string) (string, error) {
	dir := flag
	if dir == "" {
		dir = os.Getenv(EnvDataDir)
	}
	if dir == "" {
		dir = DefaultDataDir()
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, 0o700)
}

// Logger writes JSON lines to <dataDir>/whatsapp-mcp.log and a readable copy to
// console (if non-nil). The caller closes the returned file.
//
// stdout carries the MCP protocol in serve mode, so console must never be os.Stdout there.
func Logger(dataDir string, console *os.File, level zerolog.Level) (zerolog.Logger, io.Closer, error) {
	f, err := os.OpenFile(filepath.Join(dataDir, "whatsapp-mcp.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return zerolog.Nop(), nil, err
	}
	var out io.Writer = f
	if console != nil {
		out = zerolog.MultiLevelWriter(f, zerolog.ConsoleWriter{
			Out:        console,
			TimeFormat: time.Kitchen,
			NoColor:    !isatty.IsTerminal(console.Fd()),
		})
	}
	return zerolog.New(out).Level(level).With().Timestamp().Logger(), f, nil
}
