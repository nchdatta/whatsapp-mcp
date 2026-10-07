// Package desktop registers whatsapp-mcp in Claude Desktop's config file.
package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// ServerName is the key used under "mcpServers".
const ServerName = "whatsapp"

// ConfigPath finds claude_desktop_config.json for this user. On Windows the
// Microsoft Store build keeps it in its package folder, so that wins when present.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	const file = "claude_desktop_config.json"
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		if pkgs, _ := filepath.Glob(filepath.Join(local, "Packages", "Claude_*")); len(pkgs) > 0 {
			sort.Strings(pkgs)
			return filepath.Join(pkgs[len(pkgs)-1], "LocalCache", "Roaming", "Claude", file), nil
		}
		roaming := os.Getenv("APPDATA")
		if roaming == "" {
			roaming = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(roaming, "Claude", file), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", file), nil
	default:
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "Claude", file), nil
	}
}

// Server is one mcpServers entry.
type Server struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Install adds or replaces the whatsapp entry, keeping every other setting.
// The previous file is kept as <config>.bak. It reports whether anything changed.
func Install(path string, srv Server) (bool, error) {
	cfg, raw, err := load(path)
	if err != nil {
		return false, err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	if same(servers[ServerName], srv) {
		return false, nil
	}
	servers[ServerName] = srv
	cfg["mcpServers"] = servers
	return true, save(path, cfg, raw)
}

// Uninstall removes the whatsapp entry. It reports whether it was present.
func Uninstall(path string) (bool, error) {
	cfg, raw, err := load(path)
	if err != nil {
		return false, err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if _, ok := servers[ServerName]; !ok {
		return false, nil
	}
	delete(servers, ServerName)
	return true, save(path, cfg, raw)
}

// Current returns the registered entry, if any.
func Current(path string) (*Server, error) {
	cfg, _, err := load(path)
	if err != nil {
		return nil, err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	entry, ok := servers[ServerName]
	if !ok {
		return nil, nil
	}
	b, _ := json.Marshal(entry)
	var s Server
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func load(path string) (map[string]any, []byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	cfg := map[string]any{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return cfg, raw, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep numbers exactly as written
	if err := dec.Decode(&cfg); err != nil {
		return nil, nil, fmt.Errorf("%s is not valid JSON (fix or delete it first): %w", path, err)
	}
	return cfg, raw, nil
}

func save(path string, cfg map[string]any, previous []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if previous != nil {
		if err := os.WriteFile(path+".bak", previous, 0o600); err != nil {
			return fmt.Errorf("backing up config: %w", err)
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	// Write to a temp file and rename, so a crash never leaves half a config
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// same compares an entry read from the file with srv, ignoring key order.
func same(existing any, srv Server) bool {
	var want any
	b, _ := json.Marshal(srv)
	json.Unmarshal(b, &want)
	x, _ := json.Marshal(existing)
	y, _ := json.Marshal(want)
	return bytes.Equal(x, y)
}
