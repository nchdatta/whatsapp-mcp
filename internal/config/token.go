package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const tokenFile = "http-token"

// HTTPToken returns the secret that protects `serve --http`, creating it on
// first use. rotate replaces it, invalidating old connector URLs.
func HTTPToken(dataDir string, rotate bool) (string, error) {
	path := filepath.Join(dataDir, tokenFile)
	if !rotate {
		b, err := os.ReadFile(path)
		if err == nil && len(strings.TrimSpace(string(b))) >= 32 {
			return strings.TrimSpace(string(b)), nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return token, nil
}
