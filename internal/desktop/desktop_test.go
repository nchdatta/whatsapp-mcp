package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallKeepsOtherSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	original := `{"mcpServers":{"other":{"command":"x"}},"preferences":{"sidebarMode":"epitaxy","count":12345678901234567890}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := Server{Command: `C:\bin\whatsapp-mcp.exe`, Args: []string{"serve"}}
	changed, err := Install(path, srv)
	if err != nil || !changed {
		t.Fatalf("Install = %v, %v", changed, err)
	}

	raw, _ := os.ReadFile(path)
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	servers := cfg["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("other MCP server was dropped")
	}
	if !strings.Contains(string(raw), "12345678901234567890") {
		t.Error("large number was not preserved exactly")
	}
	if !strings.Contains(string(raw), `"sidebarMode": "epitaxy"`) {
		t.Error("preferences were not preserved")
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != original {
		t.Error("backup does not match the original file")
	}

	got, err := Current(path)
	if err != nil || got == nil || got.Command != srv.Command {
		t.Fatalf("Current = %+v, %v", got, err)
	}

	if changed, _ := Install(path, srv); changed {
		t.Error("installing the same entry twice should be a no-op")
	}

	if removed, err := Uninstall(path); err != nil || !removed {
		t.Fatalf("Uninstall = %v, %v", removed, err)
	}
	if got, _ := Current(path); got != nil {
		t.Error("entry still present after Uninstall")
	}
}

func TestInstallCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Claude", "claude_desktop_config.json")
	if _, err := Install(path, Server{Command: "/bin/whatsapp-mcp", Args: []string{"serve"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := Current(path); got == nil || got.Command != "/bin/whatsapp-mcp" {
		t.Fatalf("Current = %+v", got)
	}
}

func TestInstallRejectsBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	os.WriteFile(path, []byte(`{"mcpServers": `), 0o600)
	if _, err := Install(path, Server{Command: "x"}); err == nil {
		t.Error("expected an error for invalid JSON")
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{"mcpServers": ` {
		t.Error("broken file must be left untouched")
	}
}
