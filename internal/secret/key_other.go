//go:build !windows && !darwin

package secret

import (
	"errors"
	"os"
)

// On Linux and other systems the master key is a file only this user can
// read. Copies of the databases are unreadable without it, but anyone who can
// read the whole data directory can read the key too; full-disk encryption
// gives stronger protection.

func loadMaster(dataDir string) ([]byte, error) {
	data, err := os.ReadFile(keyFile(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoKey
	}
	return data, err
}

func saveMaster(dataDir string, key []byte) error {
	return os.WriteFile(keyFile(dataDir), key, 0o600)
}

func deleteMaster(dataDir string) error {
	err := os.Remove(keyFile(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
