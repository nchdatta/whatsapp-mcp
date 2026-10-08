package update

import (
	"context"
	"os"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		current, latest string
		want            bool
	}{
		{"v1.7.0", "v1.7.1", true},
		{"v1.7.0", "v1.10.0", true},
		{"v1.7.0", "v2.0.0", true},
		{"v1.7.0", "v1.7.0", false},
		{"v1.8.0", "v1.7.9", false},
		{"dev", "v1.7.0", false},
		{"v1.7.0", "", false},
		{"v1.7.0-rc1", "v1.7.0", false},
	} {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

// Downloads a real release; run with WHATSAPP_MCP_LIVE=1.
func TestDownloadLive(t *testing.T) {
	if os.Getenv("WHATSAPP_MCP_LIVE") == "" {
		t.Skip("set WHATSAPP_MCP_LIVE=1 to download a real release")
	}
	tag, err := Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path, err := Download(context.Background(), tag, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s -> %s", tag, path)
}
