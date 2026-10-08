// Package update finds, downloads and verifies newer releases.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const repo = "nchdatta/whatsapp-mcp"

// Latest returns the tag of the newest release, e.g. "v1.7.0".
func Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "whatsapp-mcp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("checking for updates: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checking for updates: GitHub answered %s", resp.Status)
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("checking for updates: %w", err)
	}
	if rel.Tag == "" {
		return "", fmt.Errorf("checking for updates: no release found")
	}
	return rel.Tag, nil
}

// Newer reports whether latest is a higher version than current. Development
// builds ("dev") are never reported as outdated.
func Newer(current, latest string) bool {
	c, ok1 := parse(current)
	l, ok2 := parse(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// AssetName is the release file built for this system.
func AssetName() string {
	name := "whatsapp-mcp-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Download fetches this system's build of release tag into dir, checks it
// against the release's SHA256SUMS, and returns its path.
func Download(ctx context.Context, tag, dir string) (string, error) {
	base := "https://github.com/" + repo + "/releases/download/" + tag + "/"
	asset := AssetName()

	sums, err := fetch(ctx, base+"SHA256SUMS")
	if err != nil {
		return "", err
	}
	want := ""
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return "", fmt.Errorf("%s is missing from the release's SHA256SUMS", asset)
	}

	data, err := fetch(ctx, base+asset)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return "", fmt.Errorf("checksum mismatch for %s; not installing it", asset)
	}
	path := filepath.Join(dir, asset)
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "whatsapp-mcp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

var available atomic.Value // string: newest tag seen by Watch

// Watch checks for a newer release now and then once a day, until ctx ends.
func Watch(ctx context.Context) {
	for {
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if tag, err := Latest(cctx); err == nil {
			available.Store(tag)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

// Available returns the newest release tag found by Watch if it's newer
// than current, else "".
func Available(current string) string {
	tag, _ := available.Load().(string)
	if Newer(current, tag) {
		return tag
	}
	return ""
}
