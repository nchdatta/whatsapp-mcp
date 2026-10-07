package secret

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestSealOpen(t *testing.T) {
	k := ForTest()
	msg := []byte("hello")
	sealed := k.Seal(msg)
	if bytes.Contains(sealed, msg) || !IsSealed(sealed) {
		t.Fatal("not encrypted")
	}
	got, err := k.Open(sealed)
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("Open = %q, %v", got, err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := k.Open(sealed); err == nil {
		t.Fatal("tampered data decrypted")
	}
}

func TestFiles(t *testing.T) {
	k := ForTest()
	path := filepath.Join(t.TempDir(), "f.enc")
	if err := k.WriteFile(path, []byte("data")); err != nil {
		t.Fatal(err)
	}
	if !IsSealedFile(path) {
		t.Fatal("file not sealed")
	}
	got, err := k.ReadFile(path)
	if err != nil || string(got) != "data" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
}

func TestLoadIsStable(t *testing.T) {
	dir := t.TempDir()
	if Exists(dir) {
		t.Fatal("key exists before Load")
	}
	a, err := Load(dir)
	if err != nil {
		t.Skip("no OS key storage here:", err)
	}
	t.Cleanup(func() { Delete(dir) })
	b, err := Load(dir)
	if err != nil || a.DBHex != b.DBHex {
		t.Fatalf("second Load differs: %v", err)
	}
	if !Exists(dir) {
		t.Fatal("Exists = false after Load")
	}
}
