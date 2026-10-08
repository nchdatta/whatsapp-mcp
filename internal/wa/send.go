package wa

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/nchdatta/whatsapp-mcp/internal/audio"
	"github.com/nchdatta/whatsapp-mcp/internal/store"
)

var nonDigits = regexp.MustCompile(`[^\d]`)

// Recipient resolves "to" into a chat JID. It accepts a JID, a phone number in
// any common format, or a contact or chat name.
func (s *Service) Recipient(ctx context.Context, to string) (types.JID, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return types.JID{}, fmt.Errorf("recipient is empty")
	}
	if strings.Contains(to, "@") {
		return types.ParseJID(to)
	}

	if digits := nonDigits.ReplaceAllString(to, ""); len(digits) >= 7 && len(digits) >= len(strings.TrimSpace(to))/2 {
		res, err := s.Client.IsOnWhatsApp(ctx, []string{"+" + digits})
		if err != nil {
			return types.JID{}, fmt.Errorf("checking %s on WhatsApp: %w", to, err)
		}
		if len(res) == 0 || !res[0].IsIn {
			return types.JID{}, fmt.Errorf("+%s is not on WhatsApp", digits)
		}
		return res[0].JID, nil
	}

	jid, err := s.chatByName(ctx, to)
	if err != nil {
		return types.JID{}, err
	}
	return types.ParseJID(jid)
}

var mentionToken = regexp.MustCompile(`@\+?(\d{7,15})\b`)

// withMentions turns "@<number>" in text into real mentions. The number may
// be a phone number or a hidden LID; in groups it's matched against the
// member list and rewritten to the ID the group addresses that member by.
func (s *Service) withMentions(ctx context.Context, chat types.JID, text string) (string, []string) {
	if !mentionToken.MatchString(text) {
		return text, nil
	}
	var g *groupMeta
	if chat.Server == types.GroupServer {
		g = s.group(ctx, chat)
	}
	var jids []string
	out := mentionToken.ReplaceAllStringFunc(text, func(tok string) string {
		num := mentionToken.FindStringSubmatch(tok)[1]
		target := s.mentionTarget(ctx, g, num)
		jids = append(jids, target.String())
		return "@" + target.User
	})
	return out, jids
}

// mentionTarget resolves a mentioned number (phone or LID) to a JID.
func (s *Service) mentionTarget(ctx context.Context, g *groupMeta, num string) types.JID {
	if g != nil {
		if j, ok := g.members[num]; ok {
			return j
		}
	}
	pn := types.NewJID(num, types.DefaultUserServer)
	lid := types.NewJID(num, types.HiddenUserServer)
	// A known LID: use it directly, or its phone number in phone-addressed groups
	if p, err := s.Client.Store.LIDs.GetPNForLID(ctx, lid); err == nil && !p.IsEmpty() {
		if g != nil && g.lidMode {
			return lid
		}
		return p
	}
	if g != nil && g.lidMode {
		if l, err := s.Client.Store.LIDs.GetLIDForPN(ctx, pn); err == nil && !l.IsEmpty() {
			return l
		}
	}
	return pn
}

// SendText sends a text message and records it in the local history.
// replyTo, if set, is the ID of a message in that chat to quote.
func (s *Service) SendText(ctx context.Context, to, text, replyTo string) (string, error) {
	if err := s.Online(ctx); err != nil {
		return "", err
	}
	chat, err := s.Recipient(ctx, to)
	if err != nil {
		return "", err
	}
	body, mentions := s.withMentions(ctx, chat, text)
	record := &store.Message{Body: body}
	msg := &waE2E.Message{}
	var ci *waE2E.ContextInfo
	if len(mentions) > 0 {
		ci = &waE2E.ContextInfo{MentionedJID: mentions}
	}
	if replyTo != "" {
		quoted, err := s.History.Message(ctx, chat.String(), replyTo)
		if err != nil {
			return "", fmt.Errorf("reply_to: message %s in %s: %w", replyTo, chat, err)
		}
		if ci == nil {
			ci = &waE2E.ContextInfo{}
		}
		sender := quoted.SenderJID
		if quoted.FromMe && s.Client.Store.ID != nil {
			sender = s.Client.Store.ID.ToNonAD().String()
		}
		quotedText := quoted.Body
		if quotedText == "" && quoted.MediaKind != "" {
			quotedText = "[" + quoted.MediaKind + "]"
		}
		ci.StanzaID = proto.String(quoted.ID)
		ci.Participant = proto.String(sender)
		ci.QuotedMessage = &waE2E.Message{Conversation: proto.String(quotedText)}
		record.QuotedID, record.QuotedSender, record.QuotedBody = quoted.ID, sender, quotedText
	}
	if ci != nil {
		msg.ExtendedTextMessage = &waE2E.ExtendedTextMessage{Text: proto.String(body), ContextInfo: ci}
	} else {
		msg.Conversation = proto.String(body)
	}
	return s.send(ctx, chat, msg, record)
}

// SendFile uploads and sends a file. Images, videos and audio are sent as
// such; anything else as a document. voice turns audio into a voice note,
// converting it to Opus with ffmpeg if needed.
func (s *Service) SendFile(ctx context.Context, to, path, caption string, voice bool) (string, error) {
	if err := s.Online(ctx); err != nil {
		return "", err
	}
	chat, err := s.Recipient(ctx, to)
	if err != nil {
		return "", err
	}

	if voice && !audio.IsOpus(path) {
		converted, cleanup, err := audio.ToOpus(ctx, path)
		if err != nil {
			return "", err
		}
		defer cleanup()
		path = converted
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	name := filepath.Base(path)

	kind, mediaType := "document", whatsmeow.MediaDocument
	switch {
	case voice:
		kind, mediaType, mimeType = "voice", whatsmeow.MediaAudio, "audio/ogg; codecs=opus"
	case strings.HasPrefix(mimeType, "image/") && mimeType != "image/svg+xml":
		kind, mediaType = "image", whatsmeow.MediaImage
	case strings.HasPrefix(mimeType, "video/"):
		kind, mediaType = "video", whatsmeow.MediaVideo
	case strings.HasPrefix(mimeType, "audio/"):
		kind, mediaType = "audio", whatsmeow.MediaAudio
	}

	up, err := s.Client.Upload(ctx, data, mediaType)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	size := uint64(len(data))
	msg := &waE2E.Message{}
	switch kind {
	case "image":
		msg.ImageMessage = &waE2E.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256, FileLength: &size,
			Mimetype: proto.String(mimeType), Caption: optional(caption),
		}
	case "video":
		msg.VideoMessage = &waE2E.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256, FileLength: &size,
			Mimetype: proto.String(mimeType), Caption: optional(caption),
		}
	case "audio", "voice":
		am := &waE2E.AudioMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256, FileLength: &size,
			Mimetype: proto.String(mimeType),
		}
		if voice {
			info, err := audio.Inspect(data)
			if err != nil {
				return "", fmt.Errorf("reading voice note: %w", err)
			}
			am.PTT = proto.Bool(true)
			am.Seconds = proto.Uint32(uint32(info.Duration.Seconds() + 0.5))
			am.Waveform = info.Waveform
		}
		msg.AudioMessage = am
	default:
		msg.DocumentMessage = &waE2E.DocumentMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256, FileLength: &size,
			Mimetype: proto.String(mimeType), FileName: proto.String(name), Title: proto.String(name),
			Caption: optional(caption),
		}
	}

	// Voice notes from temp files have no meaningful local name
	local, _ := filepath.Abs(path)
	if voice {
		local = ""
	}
	return s.send(ctx, chat, msg, &store.Message{
		Body: caption, MediaKind: kind, MediaMime: mimeType, MediaName: name, MediaSize: int64(size), MediaFile: local,
	})
}

func (s *Service) send(ctx context.Context, chat types.JID, msg *waE2E.Message, record *store.Message) (string, error) {
	resp, err := s.Client.SendMessage(ctx, chat, msg)
	if err != nil {
		return "", fmt.Errorf("send: %w", err)
	}
	// WhatsApp doesn't echo our own sends back, so record them here
	record.ChatJID, record.ID, record.FromMe, record.SentAt = chat.String(), resp.ID, true, resp.Timestamp
	if s.Client.Store.ID != nil {
		record.SenderJID = s.Client.Store.ID.ToNonAD().String()
	}
	s.History.PutChat(ctx, chat.String(), "", chat.Server == types.GroupServer, resp.Timestamp)
	s.History.MarkRead(ctx, chat.String(), resp.Timestamp) // replying reads the chat, as in WhatsApp
	if err := s.History.PutMessage(ctx, record); err != nil {
		s.log.Warn().Err(err).Msg("Recording sent message failed")
	}
	return resp.ID, nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// MarkChatRead marks a chat's unread messages as read, here and on WhatsApp
// (the senders see blue ticks if read receipts are on). It returns how many
// messages were unread.
func (s *Service) MarkChatRead(ctx context.Context, chatRef string) (int, error) {
	jid, err := s.ResolveChat(ctx, chatRef)
	if err != nil {
		return 0, err
	}
	unread, err := s.History.Unread(ctx, jid, 1000)
	if err != nil || len(unread) == 0 {
		return 0, err
	}
	if err := s.Online(ctx); err != nil {
		return 0, err
	}
	chat, err := types.ParseJID(jid)
	if err != nil {
		return 0, err
	}
	// Receipts go out per sender
	bySender := map[string][]types.MessageID{}
	for _, m := range unread {
		bySender[m.SenderJID] = append(bySender[m.SenderJID], m.ID)
	}
	for sender, ids := range bySender {
		var from types.JID
		if chat.Server == types.GroupServer {
			if from, err = types.ParseJID(sender); err != nil {
				continue
			}
		}
		if err := s.Client.MarkRead(ctx, ids, time.Now(), chat, from); err != nil {
			return 0, fmt.Errorf("sending read receipts: %w", err)
		}
	}
	return len(unread), s.History.MarkRead(ctx, jid, unread[len(unread)-1].SentAt)
}
