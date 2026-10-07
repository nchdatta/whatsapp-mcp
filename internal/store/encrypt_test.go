package store

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestDatabaseIsEncrypted(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir, testKey)
	must(t, err)
	must(t, s.PutChat(ctx, "c@s.whatsapp.net", "Secret chat title", false, time.Now()))
	s.Close()

	raw := readAll(t, dir)
	if bytes.Contains(raw, []byte("Secret chat title")) || bytes.HasPrefix(raw, sqliteHeader) {
		t.Fatal("database is stored in plain text")
	}
	if _, err := Open(ctx, dir, strings.Repeat("ff", 32)); err == nil {
		// Opening succeeds lazily; reading must fail
		s2, _ := Open(ctx, dir, strings.Repeat("ff", 32))
		if s2 != nil {
			if _, err := s2.Chats(ctx, "", nil, 10, 0); err == nil {
				t.Fatal("read with the wrong key")
			}
			s2.Close()
		}
	}
}

func TestEncryptExistingDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	plain, err := sql.Open(Driver, "file:"+filepath.ToSlash(path)+"?_pragma=journal_mode(WAL)")
	must(t, err)
	_, err = plain.ExecContext(ctx, schema)
	must(t, err)
	_, err = plain.ExecContext(ctx, `INSERT INTO chat (jid, title) VALUES ('c@s.whatsapp.net', 'Old plain title')`)
	must(t, err)
	plain.Close()
	if !IsPlaintext(path) {
		t.Fatal("setup: expected a plain database")
	}

	must(t, Encrypt(ctx, path, testKey))
	if IsPlaintext(path) || bytes.Contains(readAll(t, dir), []byte("Old plain title")) {
		t.Fatal("still plain text after Encrypt")
	}
	s, err := Open(ctx, dir, testKey)
	must(t, err)
	defer s.Close()
	if got := s.ChatTitle(ctx, "c@s.whatsapp.net"); got != "Old plain title" {
		t.Fatalf("after Encrypt, title = %q", got)
	}
	// Running it again is a no-op
	must(t, Encrypt(ctx, path, testKey))
}

// readAll concatenates the database and its WAL.
func readAll(t *testing.T, dir string) []byte {
	t.Helper()
	var all []byte
	for _, name := range []string{FileName, FileName + "-wal"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			all = append(all, b...)
		}
	}
	return all
}
