package wa

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/nchdatta/whatsapp-mcp/internal/store"
)

func (s *Service) onEvent(evt any) {
	ctx := context.Background()
	switch e := evt.(type) {
	case *events.Message:
		s.ingest(ctx, e, "", true)
	case *events.HistorySync:
		s.ingestHistory(ctx, e)
	case *events.Receipt:
		// The user read the chat on another device
		if e.IsFromMe && (e.Type == types.ReceiptTypeRead || e.Type == types.ReceiptTypeReadSelf) {
			s.History.MarkRead(ctx, e.Chat.String(), e.Timestamp)
		}
	case *events.GroupInfo:
		if e.Name != nil {
			s.groups.Delete(e.JID.String())
			s.History.SetChatTitle(ctx, e.JID.String(), e.Name.Name)
		}
	case *events.Connected:
		s.log.Info().Msg("Connected to WhatsApp")
	case *events.Disconnected:
		s.log.Warn().Msg("Disconnected from WhatsApp; reconnecting")
	case *events.LoggedOut:
		s.forget()
		s.log.Error().Str("reason", e.Reason.String()).Msg("This device was unlinked; run `whatsapp-mcp login` again")
	case *events.StreamReplaced:
		s.log.Info().Msg("Another copy of whatsapp-mcp took the WhatsApp connection; this one reconnects when a tool needs it")
	}
}

// ingest stores one message. titleHint is a chat name known from context
// (history sync), and live marks messages arriving in real time.
func (s *Service) ingest(ctx context.Context, e *events.Message, titleHint string, live bool) {
	body, media := describe(e.Message)
	if body == "" && media == nil {
		return // reactions, receipts, protocol messages, ...
	}

	chat := e.Info.Chat
	isGroup := chat.Server == types.GroupServer
	title := titleHint
	if title == "" && s.History.ChatTitle(ctx, chat.String()) == "" {
		title = s.chatTitle(ctx, chat)
	}
	if err := s.History.PutChat(ctx, chat.String(), title, isGroup, e.Info.Timestamp); err != nil {
		s.log.Warn().Err(err).Str("chat", chat.String()).Msg("Saving chat failed")
		return
	}

	// Prefer the phone-number identity when the sender is shown by LID
	sender := e.Info.Sender.ToNonAD()
	if sender.Server == types.HiddenUserServer && !e.Info.SenderAlt.IsEmpty() {
		sender = e.Info.SenderAlt.ToNonAD()
	}
	senderName := ""
	if !e.Info.IsFromMe {
		senderName = e.Info.PushName
		if senderName == "" || senderName == "-" {
			senderName = s.contactName(ctx, sender)
		}
	}

	m := &store.Message{
		ChatJID:    chat.String(),
		ID:         e.Info.ID,
		SenderJID:  sender.String(),
		SenderName: senderName,
		FromMe:     e.Info.IsFromMe,
		SentAt:     e.Info.Timestamp,
		Body:       body,
	}
	if ci := contextInfo(e.Message); ci.GetStanzaID() != "" {
		m.QuotedID, m.QuotedSender = ci.GetStanzaID(), ci.GetParticipant()
		m.QuotedBody = summary(ci.GetQuotedMessage())
	}
	if media != nil {
		m.MediaKind, m.MediaMime, m.MediaName, m.MediaSize, m.MediaRef =
			media.kind, media.mime, media.name, int64(media.size), media.ref
	}
	if err := s.History.PutMessage(ctx, m); err != nil {
		s.log.Warn().Err(err).Str("id", m.ID).Msg("Saving message failed")
		return
	}

	if live {
		s.log.Info().Str("chat", m.ChatJID).Str("from", m.SenderJID).Str("media", m.MediaKind).Msg("Message")
		if media != nil && !m.FromMe && media.size <= AutoSaveLimit {
			go func() {
				if _, err := s.SaveMedia(context.Background(), m.ChatJID, m.ID); err != nil {
					s.log.Warn().Err(err).Str("id", m.ID).Msg("Saving attachment failed")
				}
			}()
		}
	}
}

func (s *Service) ingestHistory(ctx context.Context, e *events.HistorySync) {
	stored := 0
	for _, conv := range e.Data.GetConversations() {
		chat, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		title := conv.GetName()
		if title == "" {
			title = conv.GetDisplayName()
		}
		for _, hm := range conv.GetMessages() {
			evt, err := s.Client.ParseWebMessage(chat, hm.GetMessage())
			if err != nil {
				continue
			}
			s.ingest(ctx, evt, title, false)
			stored++
		}
		s.History.KeepUnread(ctx, chat.String(), int(conv.GetUnreadCount()))
	}
	s.log.Info().Int("conversations", len(e.Data.GetConversations())).Int("messages", stored).Msg("History sync")
}

// attachment is what we keep about a message's media.
type attachment struct {
	kind string // image, video, audio, voice, document, sticker
	mime string
	name string
	size uint64
	ref  []byte // the media sub-message, serialized
}

// describe extracts a readable body and any attachment from a message.
func describe(m *waE2E.Message) (string, *attachment) {
	if m == nil {
		return "", nil
	}
	switch {
	case m.GetConversation() != "":
		return m.GetConversation(), nil
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetText(), nil
	case m.GetImageMessage() != nil:
		x := m.GetImageMessage()
		return x.GetCaption(), newAttachment("image", x.GetMimetype(), "", x.GetFileLength(), x)
	case m.GetVideoMessage() != nil:
		x := m.GetVideoMessage()
		return x.GetCaption(), newAttachment("video", x.GetMimetype(), "", x.GetFileLength(), x)
	case m.GetAudioMessage() != nil:
		x := m.GetAudioMessage()
		kind := "audio"
		if x.GetPTT() {
			kind = "voice"
		}
		return "", newAttachment(kind, x.GetMimetype(), "", x.GetFileLength(), x)
	case m.GetDocumentMessage() != nil:
		x := m.GetDocumentMessage()
		return x.GetCaption(), newAttachment("document", x.GetMimetype(), x.GetFileName(), x.GetFileLength(), x)
	case m.GetDocumentWithCaptionMessage().GetMessage().GetDocumentMessage() != nil:
		return describe(m.GetDocumentWithCaptionMessage().GetMessage())
	case m.GetStickerMessage() != nil:
		x := m.GetStickerMessage()
		return "", newAttachment("sticker", x.GetMimetype(), "", x.GetFileLength(), x)
	case m.GetLocationMessage() != nil:
		x := m.GetLocationMessage()
		return strings.TrimSpace(fmt.Sprintf("[location %.6f,%.6f] %s %s", x.GetDegreesLatitude(), x.GetDegreesLongitude(), x.GetName(), x.GetAddress())), nil
	case m.GetLiveLocationMessage() != nil:
		x := m.GetLiveLocationMessage()
		return fmt.Sprintf("[live location %.6f,%.6f]", x.GetDegreesLatitude(), x.GetDegreesLongitude()), nil
	case m.GetContactMessage() != nil:
		return "[contact card] " + m.GetContactMessage().GetDisplayName(), nil
	case m.GetPollCreationMessage() != nil:
		return pollText(m.GetPollCreationMessage()), nil
	case m.GetPollCreationMessageV3() != nil:
		return pollText(m.GetPollCreationMessageV3()), nil
	}
	return "", nil
}

func pollText(p *waE2E.PollCreationMessage) string {
	var opts []string
	for _, o := range p.GetOptions() {
		opts = append(opts, o.GetOptionName())
	}
	return fmt.Sprintf("[poll] %s (%s)", p.GetName(), strings.Join(opts, " / "))
}

// contextInfo returns the reply/mention context of a message, if any.
func contextInfo(m *waE2E.Message) *waE2E.ContextInfo {
	switch {
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		return m.GetAudioMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		return m.GetDocumentMessage().GetContextInfo()
	case m.GetDocumentWithCaptionMessage() != nil:
		return contextInfo(m.GetDocumentWithCaptionMessage().GetMessage())
	case m.GetStickerMessage() != nil:
		return m.GetStickerMessage().GetContextInfo()
	}
	return nil
}

// summary is a one-line description of a message, e.g. for a quoted reply.
func summary(m *waE2E.Message) string {
	body, media := describe(m)
	if media != nil {
		body = strings.TrimSpace("[" + media.kind + "] " + body)
	}
	return body
}
