package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/nchdatta/whatsapp-mcp/internal/secret"
)

const tokenFile = "http-token"

// HTTPToken returns the secret that protects `serve --http`, creating it on
// first use. It's stored encrypted. rotate replaces it, invalidating old
// connector URLs.
func HTTPToken(dataDir string, rotate bool) (string, error) {
	keys, err := secret.Load(dataDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dataDir, tokenFile)
	if !rotate {
		data, err := os.ReadFile(path)
		switch {
		case err == nil && secret.IsSealed(data):
			plain, err := keys.Open(data)
			if err != nil {
				return "", err
			}
			return string(plain), nil
		case err == nil && len(strings.TrimSpace(string(data))) >= 32:
			// Written by a version without encryption: keep it, encrypted
			token := strings.TrimSpace(string(data))
			return token, keys.WriteFile(path, []byte(token))
		case err != nil && !errors.Is(err, os.ErrNotExist):
			return "", err
		}
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	return token, keys.WriteFile(path, []byte(token))
}
