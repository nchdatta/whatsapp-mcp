//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

// removeProgram deletes the installed binary, and its folder if that's now
// empty. A running binary can be deleted here.
func removeProgram(bin string) error {
	if err := os.Remove(bin); err != nil {
		return err
	}
	os.Remove(filepath.Dir(bin)) // fails harmlessly if not empty, e.g. ~/.local/bin
	return nil
}
