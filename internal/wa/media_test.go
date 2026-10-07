package wa

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/nchdatta/whatsapp-mcp/internal/secret"
	"github.com/nchdatta/whatsapp-mcp/internal/store"
)

func TestOldMediaIsEncryptedAndViewable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keys := secret.ForTest()
	history, err := store.Open(ctx, dir, keys.DBHex)
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	s := &Service{History: history, Keys: keys, mediaDir: filepath.Join(dir, "media"), log: zerolog.Nop()}
	defer s.removeViewDir()

	// An attachment saved in plain text by an older version
	chat := "c@s.whatsapp.net"
	old := filepath.Join(s.mediaDir, "c_s.whatsapp.net", "ID1.jpg")
	os.MkdirAll(filepath.Dir(old), 0o700)
	photo := []byte("not really a jpeg")
	os.WriteFile(old, photo, 0o600)
	history.PutChat(ctx, chat, "", false, time.Now())
	history.PutMessage(ctx, &store.Message{ChatJID: chat, ID: "ID1", SenderJID: chat, SentAt: time.Now(), MediaKind: "image", MediaFile: old})

	s.encryptOldMedia(ctx)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("plain file still there")
	}
	m, err := history.Message(ctx, chat, "ID1")
	if err != nil || m.MediaFile != old+encSuffix {
		t.Fatalf("media_file = %q, %v", m.MediaFile, err)
	}
	raw, _ := os.ReadFile(m.MediaFile)
	if bytes.Contains(raw, photo) {
		t.Fatal("attachment stored in plain text")
	}

	view := s.ViewSaved(m.MediaFile)
	got, err := os.ReadFile(view)
	if err != nil || !bytes.Equal(got, photo) {
		t.Fatalf("view %q = %q, %v", view, got, err)
	}
	if data, err := s.ReadMedia(m.MediaFile); err != nil || !bytes.Equal(data, photo) {
		t.Fatalf("ReadMedia = %q, %v", data, err)
	}
	s.removeViewDir()
	if _, err := os.Stat(view); !os.IsNotExist(err) {
		t.Fatal("readable copy left behind")
	}
}
