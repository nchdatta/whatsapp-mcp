package wa

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

// SaveMedia downloads a message's attachment into the media folder (once)
// and returns its path.
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
	path := filepath.Join(dir, fileName(m.ID, m.MediaName, m.MediaMime))
	if err := os.WriteFile(path, data, 0o600); err != nil {
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
