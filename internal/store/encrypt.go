package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// sqliteHeader starts every unencrypted SQLite database file.
var sqliteHeader = []byte("SQLite format 3\x00")

// IsPlaintext reports whether path is an unencrypted SQLite database.
func IsPlaintext(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, len(sqliteHeader))
	_, err = io.ReadFull(f, head)
	return err == nil && bytes.Equal(head, sqliteHeader)
}

// Encrypt converts an unencrypted database at path into an encrypted one in
// place. It does nothing when path doesn't exist or is already encrypted. No
// other process may have the database open.
func Encrypt(ctx context.Context, path, keyHex string) error {
	if !IsPlaintext(path) {
		return nil
	}
	tmp := path + ".encrypting"
	removeDB(tmp)

	plain, err := sql.Open(Driver, "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	// VACUUM INTO writes a complete copy, including anything still in the WAL
	target := "file:" + filepath.ToSlash(tmp) + "?vfs=adiantum&hexkey=" + keyHex
	_, err = plain.ExecContext(ctx, "VACUUM INTO "+quote(target))
	plain.Close()
	if err != nil {
		removeDB(tmp)
		return fmt.Errorf("encrypting %s: %w", filepath.Base(path), err)
	}
	if err := verify(ctx, tmp, keyHex); err != nil {
		removeDB(tmp)
		return fmt.Errorf("encrypting %s: %w", filepath.Base(path), err)
	}
	if err := removeDB(path); err != nil {
		removeDB(tmp)
		return fmt.Errorf("replacing %s (is Claude Desktop running?): %w", filepath.Base(path), err)
	}
	return os.Rename(tmp, path)
}

func verify(ctx context.Context, path, keyHex string) error {
	db, err := sql.Open(Driver, "file:"+filepath.ToSlash(path)+"?vfs=adiantum&hexkey="+keyHex)
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return errors.New("integrity check failed: " + result)
	}
	return nil
}

// removeDB deletes a database file and its WAL and shared-memory files.
func removeDB(path string) error {
	var errs []error
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
