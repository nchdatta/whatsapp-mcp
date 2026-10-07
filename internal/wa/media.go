package wa

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// newAttachment serializes a media sub-message; it carries everything needed
// to download and decrypt the file later.
func newAttachment(kind, mimeType, name string, size uint64, msg proto.Message) *attachment {
	ref, err := proto.Marshal(msg)
	if err != nil {
		ref = nil
	}
	return &attachment{kind: kind, mime: mimeType, name: name, size: size, ref: ref}
}

// downloadable rebuilds the media sub-message saved by newAttachment.
func downloadable(kind string, ref []byte) (whatsmeow.DownloadableMessage, error) {
	var msg interface {
		proto.Message
		whatsmeow.DownloadableMessage
	}
	switch kind {
	case "image":
		msg = &waE2E.ImageMessage{}
	case "video":
		msg = &waE2E.VideoMessage{}
	case "audio", "voice":
		msg = &waE2E.AudioMessage{}
	case "document":
		msg = &waE2E.DocumentMessage{}
	case "sticker":
		msg = &waE2E.StickerMessage{}
	default:
		return nil, fmt.Errorf("unknown media kind %q", kind)
	}
	if err := proto.Unmarshal(ref, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

var unsafeName = regexp.MustCompile(`[^\w.\-]+`)

// encSuffix marks encrypted attachments in the media folder.
const encSuffix = ".enc"

// ViewMedia makes sure a message's attachment is saved and returns the path
// of a readable copy: encrypted attachments are decrypted into a temporary
// folder that is removed when whatsapp-mcp exits.
func (s *Service) ViewMedia(ctx context.Context, chatJID, msgID string) (string, error) {
	stored, err := s.SaveMedia(ctx, chatJID, msgID)
	if err != nil {
		return "", err
	}
	return s.viewable(stored)
}

// ViewSaved is ViewMedia for an attachment that's already saved; it never
// downloads. It returns "" when there's nothing to show yet.
func (s *Service) ViewSaved(stored string) string {
	if stored == "" {
		return ""
	}
	path, err := s.viewable(stored)
	if err != nil {
		return ""
	}
	return path
}

func (s *Service) viewable(stored string) (string, error) {
	if !strings.HasSuffix(stored, encSuffix) {
		return stored, nil // a file the user sent from their own disk
	}
	dir, err := s.ensureViewDir()
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, filepath.Base(filepath.Dir(stored))+"-"+strings.TrimSuffix(filepath.Base(stored), encSuffix))
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	data, err := s.Keys.ReadFile(stored)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		return "", err
	}
	return out, nil
}

// ReadMedia returns the content of a stored or viewable attachment.
func (s *Service) ReadMedia(path string) ([]byte, error) {
	if strings.HasSuffix(path, encSuffix) {
		return s.Keys.ReadFile(path)
	}
	return os.ReadFile(path)
}

const viewPrefix = "whatsapp-mcp-view-"

func (s *Service) ensureViewDir() (string, error) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	if s.viewDir != "" {
		return s.viewDir, nil
	}
	sweepViewDirs()
	dir, err := os.MkdirTemp("", viewPrefix)
	if err != nil {
		return "", err
	}
	s.viewDir = dir
	return dir, nil
}

func (s *Service) removeViewDir() {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	if s.viewDir != "" {
		os.RemoveAll(s.viewDir)
		s.viewDir = ""
	}
}

// sweepViewDirs removes decrypted copies left behind by a process that
// didn't exit cleanly.
func sweepViewDirs() {
	entries, _ := os.ReadDir(os.TempDir())
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), viewPrefix) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 12*time.Hour {
			os.RemoveAll(filepath.Join(os.TempDir(), e.Name()))
		}
	}
}

// encryptOldMedia encrypts attachments saved by versions without encryption.
func (s *Service) encryptOldMedia(ctx context.Context) {
	filepath.WalkDir(s.mediaDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, encSuffix) || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if err := s.Keys.WriteFile(path+encSuffix, data); err != nil {
			s.log.Warn().Err(err).Str("file", path).Msg("Encrypting attachment failed")
			return nil
		}
		if err := s.History.ReplaceMediaFile(ctx, path, path+encSuffix); err != nil {
			os.Remove(path + encSuffix)
			return nil
		}
		os.Remove(path)
		return nil
	})
}

// SaveMedia downloads a message's attachment into the media folder (once),
// encrypted, and returns its path. Use ViewMedia for a readable copy.
func (s *Service) SaveMedia(ctx context.Context, chatJID, msgID string) (string, error) {
	m, err := s.History.Message(ctx, chatJID, msgID)
	if err != nil {
		return "", fmt.Errorf("message %s in %s: %w", msgID, chatJID, err)
	}
	if m.MediaKind == "" {
		return "", errors.New("this message has no attachment")
	}
	if m.MediaFile != "" {
		if _, err := os.Stat(m.MediaFile); err == nil {
			return m.MediaFile, nil
		}
	}
	if len(m.MediaRef) == 0 {
		return "", errors.New("the attachment can no longer be downloaded (no media reference stored)")
	}
	dl, err := downloadable(m.MediaKind, m.MediaRef)
	if err != nil {
		return "", err
	}
	if err := s.Online(ctx); err != nil {
		return "", err
	}
	data, err := s.Client.Download(ctx, dl)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	dir := filepath.Join(s.mediaDir, unsafeName.ReplaceAllString(chatJID, "_"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fileName(m.ID, m.MediaName, m.MediaMime)+encSuffix)
	if err := s.Keys.WriteFile(path, data); err != nil {
		return "", err
	}
	if err := s.History.SetMediaFile(ctx, chatJID, msgID, path); err != nil {
		s.log.Warn().Err(err).Msg("Recording attachment path failed")
	}
	s.log.Info().Str("path", path).Int("bytes", len(data)).Msg("Attachment saved")
	return path, nil
}

// fileName builds a safe, unique file name: <msgID>[-original name][.ext].
func fileName(msgID, original, mimeType string) string {
	base := unsafeName.ReplaceAllString(msgID, "_")
	if original != "" {
		return base + "-" + unsafeName.ReplaceAllString(filepath.Base(original), "_")
	}
	return base + extension(mimeType)
}

func extension(mimeType string) string {
	mt := strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])
	switch mt {
	case "image/jpeg":
		return ".jpg"
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "video/mp4":
		return ".mp4"
	}
	if exts, _ := mime.ExtensionsByType(mt); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}
