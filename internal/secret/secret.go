// Package secret keeps whatsapp-mcp's data encrypted at rest. A random master
// key is created per data directory and protected by the operating system
// (DPAPI on Windows, the Keychain on macOS); the database and file keys are
// derived from it.
package secret

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Keys are the keys derived from the master key.
type Keys struct {
	// DBHex is the database key, as hex for the SQLite URI.
	DBHex string
	aead  cipher.AEAD
}

// Load returns the keys for dataDir, creating and protecting a new master key
// on first use.
func Load(dataDir string) (*Keys, error) {
	master, err := loadMaster(dataDir)
	if errors.Is(err, errNoKey) {
		master = make([]byte, 32)
		if _, err := rand.Read(master); err != nil {
			return nil, err
		}
		if err := saveMaster(dataDir, master); err != nil {
			return nil, fmt.Errorf("protecting encryption key: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("loading encryption key: %w", err)
	}
	return derive(master)
}

// Exists reports whether a master key was created for dataDir.
func Exists(dataDir string) bool {
	_, err := loadMaster(dataDir)
	return err == nil
}

// Delete removes the master key. Anything encrypted with it becomes unreadable.
func Delete(dataDir string) error { return deleteMaster(dataDir) }

// ForTest returns keys derived from a fixed master key.
func ForTest() *Keys {
	k, _ := derive(bytes.Repeat([]byte{7}, 32))
	return k
}

func derive(master []byte) (*Keys, error) {
	if len(master) != 32 {
		return nil, errors.New("encryption key has the wrong size")
	}
	db, err := hkdf.Key(sha256.New, master, nil, "whatsapp-mcp database", 32)
	if err != nil {
		return nil, err
	}
	fk, err := hkdf.Key(sha256.New, master, nil, "whatsapp-mcp files", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(fk)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Keys{DBHex: hex.EncodeToString(db), aead: aead}, nil
}

// fileMagic starts every encrypted file.
var fileMagic = []byte("WAMCP-ENC1\n")

// Seal encrypts and authenticates data.
func (k *Keys) Seal(data []byte) []byte {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err) // crypto/rand doesn't fail on supported platforms
	}
	out := append(append([]byte{}, fileMagic...), nonce...)
	return k.aead.Seal(out, nonce, data, fileMagic)
}

// Open decrypts data produced by Seal.
func (k *Keys) Open(data []byte) ([]byte, error) {
	n := k.aead.NonceSize()
	if !IsSealed(data) || len(data) < len(fileMagic)+n {
		return nil, errors.New("not an encrypted whatsapp-mcp file")
	}
	data = data[len(fileMagic):]
	out, err := k.aead.Open(nil, data[:n], data[n:], fileMagic)
	if err != nil {
		return nil, errors.New("decryption failed: wrong key or damaged file")
	}
	return out, nil
}

// IsSealed reports whether data looks like Seal output.
func IsSealed(data []byte) bool { return bytes.HasPrefix(data, fileMagic) }

// WriteFile encrypts data into path, atomically.
func (k *Keys) WriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, k.Seal(data), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadFile decrypts a file written by WriteFile.
func (k *Keys) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return k.Open(data)
}

// IsSealedFile reports whether the file at path is encrypted.
func IsSealedFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, len(fileMagic))
	_, err = io.ReadFull(f, head)
	return err == nil && bytes.Equal(head, fileMagic)
}

// keyFile is where the protected master key lives on Windows and Linux.
func keyFile(dataDir string) string { return filepath.Join(dataDir, "key") }

var errNoKey = errors.New("no key")
