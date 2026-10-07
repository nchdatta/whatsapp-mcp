package secret

import (
	"encoding/hex"
	"errors"
	"os/exec"
	"strings"
)

// On macOS the master key is kept in the login Keychain.

const keychainService = "whatsapp-mcp"

// notFound is the exit code of `security` when the item doesn't exist.
const notFound = 44

func loadMaster(dataDir string) ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-a", dataDir, "-w").Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == notFound {
		return nil, errNoKey
	}
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimSpace(string(out)))
}

func saveMaster(dataDir string, key []byte) error {
	return exec.Command("security", "add-generic-password", "-U", "-s", keychainService, "-a", dataDir,
		"-l", "whatsapp-mcp data key", "-w", hex.EncodeToString(key)).Run()
}

func deleteMaster(dataDir string) error {
	err := exec.Command("security", "delete-generic-password", "-s", keychainService, "-a", dataDir).Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == notFound {
		return nil
	}
	return err
}
